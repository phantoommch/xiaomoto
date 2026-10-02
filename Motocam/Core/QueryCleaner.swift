import Foundation

struct ParsedQuery: Equatable {
    var original: String
    var cleaned: String
    var keywords: String
    var roadCode: String?
    var roadName: String?
    var wantsToll: Bool
}

struct LocatePlan: Equatable {
    var region: String?
    var poiKeywords: [String]
    var geocodeAddress: String
}

/// 12123 writes "G15643公里450米" with the trunk code glued to the stake.
struct GluedStake: Equatable {
    var roadCode: String
    var stake: String
    var kilometre: Int
}

struct StakeAttempt: Equatable {
    var address: String
    var roadCode: String?
    var kilometre: Int?
    var query: ParsedQuery
}

enum QueryCleaner {
    /// Plaza / direction words that appear on tickets but hurt place search.
    static let plazaPhrases = [
        "进口内广场", "出口内广场", "进口外广场", "出口外广场",
        "进站口", "出站口",
        "内广场", "外广场", "收费广场",
        "进口", "出口",
        "上行", "下行",
    ]

    private static let codeExpression: NSRegularExpression = {
        // G25, G15W3, G0411, S26. Not a third-party parser — ICU via Foundation.
        let pattern = "[GS]\\d{1,4}(?:[A-Z]\\d{1,2})?"
        return try! NSRegularExpression(pattern: pattern)
    }()

    static func parse(_ raw: String) -> ParsedQuery {
        let normalized = TextNormalize.collapseSpaces(TextNormalize.halfwidth(raw))
            .trimmingCharacters(in: .whitespacesAndNewlines)
        let roadCode = gluedStakes(in: normalized).first?.roadCode ?? firstRoadCode(in: normalized)
        let roadName = firstRoadName(in: normalized)

        var cleaned = normalized
        for phrase in plazaPhrases {
            cleaned = cleaned.replacingOccurrences(of: phrase, with: " ")
        }
        cleaned = cleaned.replacingOccurrences(of: "(", with: " ")
        cleaned = cleaned.replacingOccurrences(of: ")", with: " ")
        cleaned = cleaned.replacingOccurrences(of: "（", with: " ")
        cleaned = cleaned.replacingOccurrences(of: "）", with: " ")
        cleaned = TextNormalize.collapseSpaces(cleaned)
            .trimmingCharacters(in: CharacterSet.whitespaces.union(CharacterSet(charactersIn: "，,。.;；、")))

        var keywords = cleaned
        if let roadCode {
            keywords = removeOnce(roadCode, from: keywords)
        }
        if let roadName {
            keywords = removeOnce(roadName, from: keywords)
        }
        keywords = TextNormalize.collapseSpaces(keywords)
            .trimmingCharacters(in: CharacterSet.whitespaces.union(CharacterSet(charactersIn: "-_，,。.")))
        if let roadCode, isStakeRemainder(keywords) {
            let road = [roadCode, roadName].compactMap { $0 }.joined()
            if !road.isEmpty { keywords = road }
        }
        if let toll = tollName(in: keywords) {
            keywords = toll
        }
        if keywords.isEmpty {
            keywords = cleaned.isEmpty ? normalized : cleaned
        }

        let wantsToll = normalized.contains("收费站") || keywords.contains("收费站")
        return ParsedQuery(
            original: raw,
            cleaned: cleaned,
            keywords: keywords,
            roadCode: roadCode,
            roadName: roadName,
            wantsToll: wantsToll
        )
    }

    static func locatePlan(_ query: ParsedQuery) -> LocatePlan {
        let source = query.cleaned.isEmpty ? query.original : query.cleaned
        let region = adminRegion(in: source)
        let facility = facilityName(in: source)
        let glued = gluedStakes(in: source).first
        let stake = glued?.stake ?? stakeToken(in: source)
        let primary: String
        if let facility {
            primary = facility
        } else if let roadName = query.roadName, adminOnly(query.keywords) {
            primary = roadName
        } else {
            primary = query.keywords
        }
        var keywords = [primary]
        if query.roadCode != nil, let stake {
            let extra = query.roadName != nil ? (query.roadName ?? "") + stake : "\(query.roadCode ?? "") \(stake)"
            if !keywords.contains(extra) { keywords.append(extra) }
        }
        let geocode = glued.map { "\($0.roadCode) \($0.stake)" } ?? source
        return LocatePlan(region: region, poiKeywords: keywords, geocodeAddress: geocode)
    }

    /// Splits a glued ticket stake. "G15643公里450米" is G15 + 643公里, not road G1564.
    /// Trunk codes (2 digits) are tried before a 4-digit code. The caller keeps the
    /// candidate whose geocode actually contains that kilometre.
    static func gluedStakes(in text: String) -> [GluedStake] {
        let compact = text.replacingOccurrences(of: " ", with: "")
        let pattern = "([GS])(\\d{3,8})((?:公里|千米|KM)(?:\\d{1,4}(?:米|M))?)"
        guard let groups = firstMatchGroups(pattern, in: compact), groups.count >= 4 else { return [] }
        let letter = groups[1].uppercased()
        let digits = groups[2]
        let unit = groups[3]
        var found: [GluedStake] = []
        for roadLen in [2, 4, 1] {
            guard roadLen < digits.count else { continue }
            let kmText = String(digits.dropFirst(roadLen))
            if kmText.count > 1 && kmText.hasPrefix("0") { continue }
            guard let km = Int(kmText), km > 0, km <= 4_500 else { continue }
            let code = letter + String(digits.prefix(roadLen))
            found.append(GluedStake(roadCode: code, stake: kmText + unit, kilometre: km))
        }
        return found
    }

    static func stakeAttempts(_ query: ParsedQuery) -> [StakeAttempt] {
        let source = query.cleaned.isEmpty ? query.original : query.cleaned
        let glued = gluedStakes(in: source)
        if !glued.isEmpty {
            return glued.map { item in
                var adjusted = query
                adjusted.roadCode = item.roadCode
                return StakeAttempt(
                    address: "\(item.roadCode) \(item.stake)",
                    roadCode: item.roadCode,
                    kilometre: item.kilometre,
                    query: adjusted
                )
            }
        }
        return [StakeAttempt(
            address: locatePlan(query).geocodeAddress,
            roadCode: query.roadCode,
            kilometre: stakeKilometre(in: source),
            query: query
        )]
    }

    /// A kilometre stake with no toll or tunnel name. Geocode it; do not pin a random POI.
    static func bareStake(_ query: ParsedQuery) -> Bool {
        if query.wantsToll { return false }
        let source = query.cleaned.isEmpty ? query.original : query.cleaned
        if facilityName(in: source) != nil { return false }
        return needsRoadGeocode(query) && stakeKilometre(in: source) != nil
    }

    static func matchesStake(_ place: PlaceCandidate, roadCode: String?, kilometre: Int?) -> Bool {
        guard let kilometre else { return false }
        let blob = (place.name + place.address).uppercased()
        if let roadCode, !roadCode.isEmpty, !blob.contains(roadCode.uppercased()) { return false }
        return blob.contains("\(kilometre)公里") || blob.contains("\(kilometre)千米") || blob.contains("\(kilometre)KM")
    }

    static func roadName(in text: String) -> String? {
        firstRoadName(in: text)
    }

    static func adminRegion(in text: String) -> String? {
        let compact = text.replacingOccurrences(of: " ", with: "")
        for name in ["北京", "天津", "上海", "重庆"] where compact.hasPrefix(name) {
            return name
        }
        guard let province = firstGroup("^(.+?(?:省|自治区))", in: compact) else { return nil }
        let rest = String(compact.dropFirst(province.count))
        return firstGroup("^(.+?(?:自治州|地区|盟|州|市))", in: rest) ?? province
    }

    static func facilityName(in text: String) -> String? {
        let compact = text.replacingOccurrences(of: " ", with: "")
        let suffixes = ["隧道", "收费站", "服务区", "互通枢纽", "互通", "枢纽", "立交桥", "立交", "大桥", "桥"]
        let chars = Array(compact)
        for suffix in suffixes {
            guard let range = compact.range(of: suffix) else { continue }
            let index = compact.distance(from: compact.startIndex, to: range.lowerBound)
            var start = index
            var han = 0
            while start > 0 && isHan(chars[start - 1]) && han < 12 {
                start -= 1
                han += 1
            }
            if han >= 1 {
                let from = compact.index(compact.startIndex, offsetBy: start)
                var name = String(compact[from..<range.upperBound])
                for marker in ["高速公路", "快速路", "高速", "公路", "大道"] {
                    if let at = name.range(of: marker) {
                        name = String(name[at.upperBound...])
                    }
                }
                if let code = name.range(of: "^[GS]\\d{1,4}(?:[A-Z]\\d{1,2})?", options: .regularExpression) {
                    name.removeSubrange(code)
                }
                if name.count > suffix.count { return name }
            }
        }
        return nil
    }

    static func stakeToken(in text: String) -> String? {
        if let glued = gluedStakes(in: text).first { return glued.stake }
        let compact = text.replacingOccurrences(of: " ", with: "")
        if let match = firstGroup("(?i)(K\\d+(?:\\+\\d+)?)", in: compact) {
            return match.uppercased()
        }
        return firstGroup("(?i)(\\d+(?:\\.\\d+)?(?:公里|千米|KM)(?:\\d+(?:米|M))?)", in: compact)?.uppercased()
    }

    static func stakeKilometre(in text: String) -> Int? {
        if let glued = gluedStakes(in: text).first { return glued.kilometre }
        guard let stake = stakeToken(in: text) else { return nil }
        return firstGroup("(\\d+)", in: stake).flatMap(Int.init)
    }

    private static func adminOnly(_ text: String) -> Bool {
        var rest = text.replacingOccurrences(of: " ", with: "")
        let pattern = "^(?:北京市|天津市|上海市|重庆市|北京|天津|上海|重庆|.+?(?:自治区|自治州|特别行政区|地区|省|盟|州|市|新区|区|县|旗))"
        while !rest.isEmpty {
            guard let match = firstGroup("(" + pattern + ")", in: rest), !match.isEmpty else { break }
            guard rest.hasPrefix(match) else { break }
            let next = String(rest.dropFirst(match.count))
            if next == rest { break }
            rest = next
        }
        return rest.isEmpty
    }

    private static func firstMatchGroups(_ pattern: String, in text: String) -> [String]? {
        guard let regex = try? NSRegularExpression(pattern: pattern, options: [.caseInsensitive]) else { return nil }
        let range = NSRange(text.startIndex..., in: text)
        guard let match = regex.firstMatch(in: text, range: range) else { return nil }
        var groups: [String] = []
        for index in 0..<match.numberOfRanges {
            guard let part = Range(match.range(at: index), in: text) else { return nil }
            groups.append(String(text[part]))
        }
        return groups
    }

    private static func firstGroup(_ pattern: String, in text: String) -> String? {
        guard let regex = try? NSRegularExpression(pattern: pattern) else { return nil }
        let range = NSRange(text.startIndex..., in: text)
        guard let match = regex.firstMatch(in: text, range: range), match.numberOfRanges > 1,
              let group = Range(match.range(at: 1), in: text) else { return nil }
        return String(text[group])
    }

    static func needsRoadGeocode(_ query: ParsedQuery) -> Bool {
        let hasStake = query.cleaned.range(of: "(?i)公里|千米|KM", options: .regularExpression) != nil
        if !hasStake { return false }
        return query.roadCode != nil || query.roadName != nil || query.wantsToll
    }

    static func roadCodes(in text: String) -> [String] {
        let normalized = TextNormalize.halfwidth(text).uppercased()
        let range = NSRange(normalized.startIndex..., in: normalized)
        return codeExpression.matches(in: normalized, range: range).compactMap { match in
            Range(match.range, in: normalized).map { String(normalized[$0]) }
        }
    }

    private static func firstRoadCode(in text: String) -> String? {
        roadCodes(in: text).first
    }

    private static func firstRoadName(in text: String) -> String? {
        let suffixes = ["高速公路", "高速", "快速路"]
        let chars = Array(text)
        for suffix in suffixes {
            var searchStart = text.startIndex
            while let range = text.range(of: suffix, range: searchStart..<text.endIndex) {
                var index = text.distance(from: text.startIndex, to: range.lowerBound) - 1
                var hanCount = 0
                while index >= 0 && isHan(chars[index]) && hanCount < 12 {
                    if hanCount >= 2 && "区县市省州盟旗".contains(chars[index]) { break }
                    hanCount += 1
                    index -= 1
                }
                if hanCount >= 2 {
                    let nameStart = text.index(range.lowerBound, offsetBy: -(hanCount))
                    return String(text[nameStart..<range.upperBound])
                }
                searchStart = range.upperBound
            }
        }
        return nil
    }

    /// "45公里900米东向西方向" is a stake, not a place name.
    private static func isStakeRemainder(_ text: String) -> Bool {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty { return false }
        let pattern = "(?i)[0-9]+|公里|千米|KM|米|加|方向|[东南西北]向?|[（）()]|\\s"
        let left = trimmed.replacingOccurrences(of: pattern, with: "", options: .regularExpression)
        return left.isEmpty
    }

    /// "建瓯收费站" inside a kilometre stake, after the road name is removed.
    private static func tollName(in text: String) -> String? {
        guard let range = text.range(of: "[\u{4e00}-\u{9fff}]{1,8}收费站", options: .regularExpression) else {
            return nil
        }
        return String(text[range])
    }

    private static func isHan(_ character: Character) -> Bool {
        character.unicodeScalars.allSatisfy { (0x4E00...0x9FFF).contains($0.value) }
    }

    private static func removeOnce(_ token: String, from text: String) -> String {
        guard let range = text.range(of: token, options: [.caseInsensitive]) else { return text }
        var copy = text
        copy.removeSubrange(range)
        return copy
    }
}
