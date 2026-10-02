import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

struct RoadStake: Equatable {
    var km: Double
    var metres: Int?
}

struct RecogGlued: Equatable {
    var code: String
    var km: Double
    var metres: Int?
}

struct TicketParse: Equatable {
    var norm: String
    var code: String?
    var roadName: String?
    var nameCanon: String?
    var nameHow: String?
    var stakes: [RoadStake]
    var glued: [RecogGlued]
    var range: Bool
    var direction: String?
    var facility: String?
    var facilityType: String?
    var portal: String?
    var admin: [String]
}

struct OverlayFix: Equatable {
    var code: String?
    var name: String
    var km: Double
    var metres: Int
}

struct RegionSignals: Equatable {
    var provinces: [(String, String)]
    var plate: String?
    var overlay: OverlayFix?
    var overlayDir: String?
    static func == (l: RegionSignals, r: RegionSignals) -> Bool {
        l.plate == r.plate && l.overlay == r.overlay && l.overlayDir == r.overlayDir && l.provinces.elementsEqual(r.provinces, by: ==)
    }
}

struct LocatedPoint: Equatable {
    var latitude: Double
    var longitude: Double
    var endLatitude: Double?
    var endLongitude: Double?
    var method: String
    var how: String
    var address: String
    var confidence: String
    var roadCode: String
    var roadName: String
    var stake: String
    var direction: String
    var parsed: TicketParse
    var exact: Bool { confidence == "exact" }
}

enum Recog {
    private static let names: [String: String] = [
        "京哈": "G1", "京沪": "G2", "京台": "G3", "京港澳": "G4", "京昆": "G5", "京藏": "G6", "京新": "G7",
        "鹤大": "G11", "珲乌": "G12", "沈海": "G15", "丹锡": "G16", "荣乌": "G18", "青银": "G20", "青兰": "G22",
        "长深": "G25", "连霍": "G30", "济广": "G35", "宁洛": "G36", "沪陕": "G40", "沪蓉": "G42", "大广": "G45",
        "沪渝": "G50", "二广": "G55", "杭瑞": "G56", "呼北": "G59", "沪昆": "G60", "包茂": "G65", "银百": "G69",
        "福银": "G70", "泉南": "G72", "兰海": "G75", "厦蓉": "G76", "汕昆": "G78", "广昆": "G80", "银昆": "G85",
        "成渝环线": "G93", "珠三角环线": "G94", "首都环线": "G95", "杭州湾环线": "G92", "海南环线": "G98",
        "温丽": "G1513", "甬莞": "G1523", "甬金": "G1512", "常台": "G1522", "宁上": "G1514", "盐洛": "G1516",
        "青新": "G2011", "东吕": "G2516", "溧宁": "G4012", "石渝": "G5021", "德上": "G0321", "张南": "G5515",
        "杭长": "G6021", "杭金衢": "G60", "申嘉湖": "S12", "苏锡常南部": "S58", "沪常": "S58", "广河": "S2",
        "成宜昭": "S4", "威砚": "S11", "枣木": "S83", "岚菏": "S38", "芜合": "G5011", "南芜": "G4211",
        "沪武": "G4221", "淮徐": "G2513", "甬舟": "G9211", "渝黔": "G75", "渝湘": "G65", "合安": "G3",
    ]
    private static let provinces = ["北京","天津","上海","重庆","河北","山西","辽宁","吉林","黑龙江","江苏","浙江","安徽","福建","江西","山东","河南","湖北","湖南","广东","海南","四川","贵州","云南","陕西","甘肃","青海","台湾","内蒙古","广西","西藏","宁夏","新疆","香港","澳门"]
    private static let adcode = ["11":"北京","12":"天津","13":"河北","14":"山西","15":"内蒙古","21":"辽宁","22":"吉林","23":"黑龙江","31":"上海","32":"江苏","33":"浙江","34":"安徽","35":"福建","36":"江西","37":"山东","41":"河南","42":"湖北","43":"湖南","44":"广东","45":"广西","46":"海南","50":"重庆","51":"四川","52":"贵州","53":"云南","54":"西藏","61":"陕西","62":"甘肃","63":"青海","64":"宁夏","65":"新疆"]

    static func normalize(_ raw: String) -> String {
        var t = fold(raw).replacingOccurrences(of: "\r", with: "")
        t = joinBroken(t)
        t = t.replacingOccurrences(of: "\\s*\\n\\s*", with: " ", options: .regularExpression)
        t = collapseHan(t)
        for (a, b) in [("高遠","高速"),("高远","高速"),("高逮","高速"),("高建","高速"),("公理","公里"),("千來","千米"),("kM","KM"),("Km","KM"),("km","KM")] {
            t = t.replacingOccurrences(of: a, with: b)
        }
        var chars = Array(t)
        for i in chars.indices where chars[i] == "k" {
            if i + 1 >= chars.count || (chars[i + 1] != "m" && chars[i + 1] != "M") { chars[i] = "K" }
        }
        if chars.count >= 3 {
            for i in 1..<(chars.count - 1) where chars[i] == "O" || chars[i] == "o" {
                let prev = chars[i - 1], next = chars[i + 1]
                if (prev.isNumber && next.isNumber) || ((prev == "G" || prev == "S") && next.isNumber) { chars[i] = "0" }
            }
        }
        return String(chars).trimmingCharacters(in: .whitespacesAndNewlines)
    }

    static func parse(_ text: String) -> TicketParse {
        let t = normalize(text)
        var code = firstCode(t)
        let gluedAll = glued(t)
        var roadName: String?
        var canon: String?
        var how: String?
        if let nm = roadNameOf(t) {
            roadName = nm
            if let hit = nameToCode(nm, code) {
                canon = hit.1
                how = hit.2
                if code == nil { code = hit.0 }
            }
        }
        var t2 = t
        if let span = gluedAll.1 {
            let start = t.index(t.startIndex, offsetBy: span.0)
            let end = t.index(t.startIndex, offsetBy: span.1)
            t2 = String(t[..<start]) + String(repeating: " ", count: span.1 - span.0) + String(t[end...])
        }
        let stakes = stakesIn(t2)
        var range = false
        if stakes.count >= 2 {
            let between = slice(t2, stakes[0].span.1, stakes[1].span.0)
            if between.range(of: "\\s*(?:-|—|–|~|～|至|到|_)\\s*", options: .regularExpression) != nil || between.count < 12 { range = true }
        }
        var direction: String?
        let tnr = t.replacingOccurrences(of: "[\\p{Han}]{2,6}?(?:高速公路|高速|快速路)", with: " ", options: .regularExpression)
        if let m = first("([\\p{Han}]{1,6}?)方向", tnr) { direction = m[0] }
        for w in ["上行", "下行", "A道", "B道"] where t.contains(w) {
            direction = [direction, w].compactMap { $0 }.joined(separator: " ")
        }
        if let m = first("由[东南西北]向[东南西北]", t) { direction = m[0] }
        if let m = first("北向南|南向北|东向西|西向东", t), direction?.contains(m[0]) != true {
            direction = [direction, m[0]].compactMap { $0 }.joined(separator: " ")
        }
        var facility: String?
        var ftype: String?
        var portal: String?
        let fac = "收费站|服务区|停车区|隧道|特大桥|大桥|枢纽|互通|立交|站口"
        if let f = first("([\\p{Han}]{2,10}?)(\(fac))(?:([东南西北])?(入口|出口|洞口|口|进口))?", t) {
            var base = f[1].replacingOccurrences(of: "^.*?(?:高速公路|高速|省|市|县|区段|区|段)", with: "", options: .regularExpression)
            if base.isEmpty { base = f[1] }
            base = base.replacingOccurrences(of: "^(?:米|公里|千米|处|附近|往|至)+", with: "", options: .regularExpression)
            let kind = f[2] == "站口" ? "收费站" : f[2]
            facility = base + kind
            ftype = kind
            if f.count > 4 && !f[4].isEmpty { portal = f[3] + f[4] }
        }
        var t3 = t.replacingOccurrences(of: "[\\p{Han}]{2,6}?(?:高速公路|高速|快速路)", with: " ", options: .regularExpression)
        t3 = t3.replacingOccurrences(of: "[\\p{Han}]{1,10}?(?:\(fac))", with: " ", options: .regularExpression)
        var admin: [String] = []
        for a in all("([\\p{Han}]{2,7}?(?:省|自治区|市|自治州|地区|县|区))", t3) {
            let word = a[1]
            if word.count <= 8 && !word.contains("方向") && !word.contains("城区") && !word.contains("区间") { admin.append(word) }
        }
        return TicketParse(norm: t, code: code, roadName: roadName, nameCanon: canon, nameHow: how, stakes: stakes.map { RoadStake(km: $0.km, metres: $0.metres) }, glued: gluedAll.0, range: range, direction: direction, facility: facility, facilityType: ftype, portal: portal, admin: admin)
    }

    static func signals(_ full: String, overlay: String = "") -> RegionSignals {
        let t = normalize(full)
        var provs: [(String, String)] = []
        for m in all("《(?:中华人民共和国)?([\\p{Han}]{2,3}?)(?:省|自治区|市)[^》]{0,30}(?:条例|办法|规定)》", t) where provinces.contains(m[1]) {
            provs.append(("legal", m[1]))
        }
        for p in provinces {
            if t.contains(p + "交警") || t.contains(p + "高速交警") { provs.append(("sender", p)) }
            else if authority(t, p) { provs.append(("authority", p)) }
            else if t.hasPrefix(p + "省") || t.hasPrefix(p + "市") || t.hasPrefix(p + "自治区") { provs.append(("text", p)) }
        }
        if let m = first("12123\\s*(\\d{2})\\d", t), let name = adcode[m[1]] { provs.append(("sms_port", name)) }
        let plate = first("([京津沪渝冀晋辽吉黑苏浙皖闽赣鲁豫鄂湘粤琼川贵云陕甘青蒙桂藏宁新])[A-Z][A-Z0-9·]{4,6}", t)?[1]
        let o = normalize(overlay).isEmpty ? t : normalize(overlay)
        var fix: OverlayFix?
        var dir: String?
        if let m = first("地点[:：]?\\s*([GS]\\d{1,4})?([\\p{Han}]{0,6})K(\\d{1,4})\\+(\\d{1,4})", o) {
            fix = OverlayFix(code: m[1].isEmpty ? nil : m[1], name: m[2], km: Double(m[3]) ?? 0, metres: Int(m[4]) ?? 0)
        }
        if let m = first("(?:通行|行驶)方向[:：]?\\s*(上行|下行)", o) { dir = m[1] }
        if let m = first("设备编号[:：]?\\s*(\\d{6})", o), let name = adcode[String(m[1].prefix(2))] { provs.append(("device", name)) }
        return RegionSignals(provinces: provs, plate: plate, overlay: fix, overlayDir: dir)
    }

    static func level(method: String, conf: String, how: String) -> String {
        switch method {
        case "overlay-stake", "stake", "glued-stake", "tunnel-portal":
            return (conf == "low" || conf == "none" || conf.isEmpty || how.contains("metres dropped")) ? "low" : "exact"
        case "facility-poi":
            return conf == "high" ? "exact" : "low"
        default:
            return "low"
        }
    }

    static func locate(text: String, full: String, key: String) async -> LocatedPoint? {
        let parsed = parse(text.isEmpty ? full : text)
        let sig = signals(full.isEmpty ? text : full)
        let region = await regionWords(parsed, sig, full.isEmpty ? text : full, key)
        guard var hit = await locateParsed(parsed, sig, region, key), hit.latitude.isFinite else { return nil }
        hit.roadName = hit.roadName.isEmpty ? (parsed.nameCanon ?? parsed.roadName ?? "") : hit.roadName
        hit.direction = [parsed.direction, sig.overlayDir].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " ")
        hit.parsed = parsed
        hit.confidence = level(method: hit.method, conf: hit.confidence, how: hit.how)
        return hit
    }

    // MARK: locate

    private struct Marker {
        var lat, lon: Double
        var how, addr, prov, conf: String
        var ambiguous: Bool
    }

    private static func locateParsed(_ p: TicketParse, _ sig: RegionSignals, _ region: [String], _ key: String) async -> LocatedPoint? {
        let blank = LocatedPoint(latitude: .nan, longitude: .nan, endLatitude: nil, endLongitude: nil, method: "", how: "", address: "", confidence: "none", roadCode: p.code ?? "", roadName: p.nameCanon ?? "", stake: "", direction: p.direction ?? "", parsed: p)
        if let ol = sig.overlay, let code = ol.code, !code.isEmpty, let sp = await stakePoint(key, code, ol.km, ol.metres, region) {
            var hit = blank
            hit.latitude = sp.lat; hit.longitude = sp.lon; hit.method = "overlay-stake"; hit.how = sp.how; hit.address = sp.addr; hit.confidence = "high"; hit.roadCode = code; hit.stake = stakeLabel(ol.km, ol.metres)
            return hit
        }
        if !p.glued.isEmpty {
            var cands: [(Int, RecogGlued, Marker)] = []
            for g in p.glued {
                guard let sp = await stakePoint(key, g.code, g.km, g.metres, nil) else { continue }
                var score = 1
                let blob = sp.prov + sp.addr
                if !region.isEmpty && region.contains(where: { blob.contains($0) }) { score += 5 }
                else if !region.isEmpty { score -= 5 }
                cands.append((score, g, sp))
            }
            cands.sort { $0.0 > $1.0 }
            if let top = cands.first {
                var conf = top.2.conf
                if (cands.count > 1 && cands[1].0 == top.0) || (!region.isEmpty && top.0 < 6) || (region.isEmpty && cands.count > 1) { conf = "low" }
                var hit = blank
                hit.latitude = top.2.lat; hit.longitude = top.2.lon; hit.method = "glued-stake"; hit.how = top.2.how; hit.address = top.2.addr; hit.confidence = conf; hit.roadCode = top.1.code; hit.stake = stakeLabel(top.1.km, top.1.metres ?? 0)
                return hit
            }
        }
        if let code = p.code, !code.isEmpty, let firstStake = p.stakes.first {
            var sp = await stakePoint(key, code, firstStake.km, firstStake.metres, region)
            if sp == nil && !region.isEmpty { sp = await stakePoint(key, code, firstStake.km, firstStake.metres, nil) }
            if let sp {
                var conf = sp.conf
                if (sp.ambiguous || code.hasPrefix("S")) && region.isEmpty { conf = "low" }
                if !region.isEmpty && !region.contains(where: { (sp.prov + sp.addr).contains($0) }) { conf = "low" }
                var endLat: Double?
                var endLon: Double?
                if p.range, p.stakes.count > 1, let sp2 = await stakePoint(key, code, p.stakes[1].km, p.stakes[1].metres, region) {
                    endLat = sp2.lat; endLon = sp2.lon
                }
                var hit = blank
                hit.latitude = sp.lat; hit.longitude = sp.lon; hit.endLatitude = endLat; hit.endLongitude = endLon
                hit.method = "stake"; hit.how = sp.how; hit.address = sp.addr; hit.confidence = conf; hit.roadCode = code
                hit.stake = stakeLabel(firstStake.km, firstStake.metres ?? 0)
                return hit
            }
            if p.facility == nil { return nil }
        }
        if let facility = p.facility, let ftype = p.facilityType, let b = await poi(key, facility, ftype, region, p.code, p.nameCanon) {
            var conf = (b.score >= 15 || (b.score >= 10 && !region.isEmpty)) ? "high" : "medium"
            if b.far { conf = "low" }
            var lat = b.lat, lon = b.lon, method = "facility-poi", how = b.name
            let side = p.portal?.first.map(String.init) ?? ""
            if ftype == "隧道" && "东西南北".contains(side) && !side.isEmpty {
                if let tp = await osmPortal(lat, lon, side, p.direction) {
                    lat = tp.0; lon = tp.1; method = "tunnel-portal"; how = "\(b.name) \(side)口 (\(tp.2))"; conf = "high"
                } else if let tp = await tunnelStep(key, facility, b.gcjLon, b.gcjLat, side) {
                    lat = tp.0; lon = tp.1; method = "tunnel-portal"; how = "\(b.name) \(side)端"; conf = "medium"
                }
            }
            var hit = blank
            hit.latitude = lat; hit.longitude = lon; hit.method = method; hit.how = how; hit.address = b.admin; hit.confidence = conf; hit.roadCode = p.code ?? ""
            return hit
        }
        guard let geo = await get(key, "v3/geocode/geo", ["address": p.norm]),
              let row = (geo["geocodes"] as? [[String: Any]])?.first,
              let loc = split(row["location"] as? String ?? ""),
              let levelName = row["level"] as? String else { return nil }
        let w = CoordinateConverter.gcj02ToWgs84(.init(latitude: loc.1, longitude: loc.0))
        let addr = (row["formatted_address"] as? String) ?? ""
        let adminLevel = ["省","市","区县","乡镇","村庄","道路","开发区"].contains(levelName)
        let conf = (adminLevel || p.facility != nil || (!region.isEmpty && !region.contains(where: { addr.contains($0) }))) ? "low" : "medium"
        var hit = blank
        hit.latitude = w.latitude; hit.longitude = w.longitude; hit.method = "geocode"; hit.how = "level=\(levelName)"; hit.address = addr; hit.confidence = conf
        return hit
    }

    private static func stakeLabel(_ km: Double, _ metres: Int) -> String { "K\(Int(km))+\(metres)" }

    private static func kmMarker(_ key: String, _ code: String, _ km: Int, _ region: [String]?) async -> Marker? {
        guard let d = await get(key, "v3/geocode/geo", ["address": "\(code) \(km)公里"]),
              let rows = d["geocodes"] as? [[String: Any]] else { return nil }
        var hits: [(Double, Double, String, String)] = []
        for g in rows {
            let fa = (g["formatted_address"] as? String) ?? ""
            guard fa.contains(code), fa.range(of: "(?:^|[^\\d])\(km)公里", options: .regularExpression) != nil,
                  let loc = split(g["location"] as? String ?? "") else { continue }
            let w = CoordinateConverter.gcj02ToWgs84(.init(latitude: loc.1, longitude: loc.0))
            hits.append((w.latitude, w.longitude, fa, (g["province"] as? String) ?? ""))
        }
        if hits.isEmpty { return nil }
        var clusters: [[(Double, Double, String, String)]] = []
        for h in hits {
            if let i = clusters.firstIndex(where: { GeoMath.distanceMeters($0[0].0, $0[0].1, h.0, h.1) < 20000 }) {
                clusters[i].append(h)
            } else { clusters.append([h]) }
        }
        var use = clusters
        let regs = (region ?? []).filter { !$0.isEmpty }
        if !regs.isEmpty {
            let keep = clusters.filter { c in regs.contains { c[0].2.contains($0) || c[0].3.contains($0) } }
            if keep.isEmpty { return nil }
            use = keep
        }
        let c = use[0]
        let lat = c.map(\.0).reduce(0, +) / Double(c.count)
        let lon = c.map(\.1).reduce(0, +) / Double(c.count)
        return Marker(lat: lat, lon: lon, how: "", addr: c[0].2, prov: c[0].3, conf: "", ambiguous: use.count > 1)
    }

    private static func stakePoint(_ key: String, _ code: String, _ km: Double, _ metres: Int?, _ region: [String]?) async -> Marker? {
        let kmi = Int(km)
        let me = metres ?? 0
        let frac = (km - Double(kmi)) + Double(me) / 1000
        guard let a = await kmMarker(key, code, kmi, region) else {
            let steps = [1, 2, 3, 5, 8, 12]
            var lo: (Int, Marker)?
            var hi: (Int, Marker)?
            for k in steps {
                if let r = await kmMarker(key, code, kmi - k, region) { lo = (kmi - k, r); break }
            }
            for k in steps {
                if let r = await kmMarker(key, code, kmi + k, region) { hi = (kmi + k, r); break }
            }
            if let lo, let hi, hi.0 - lo.0 > 3 {
                let f = (km + Double(me) / 1000 - Double(lo.0)) / Double(hi.0 - lo.0)
                if let p = await routeInterp(key, lo.1.lat, lo.1.lon, hi.1.lat, hi.1.lon, f, Double(hi.0 - lo.0)) {
                    return Marker(lat: p.0, lon: p.1, how: "route-interp K\(lo.0)..K\(hi.0)", addr: lo.1.addr, prov: lo.1.prov, conf: "medium", ambiguous: lo.1.ambiguous)
                }
            }
            if let lo, let hi, GeoMath.distanceMeters(lo.1.lat, lo.1.lon, hi.1.lat, hi.1.lon) < 1500 * Double(hi.0 - lo.0) {
                let f = (km + Double(me) / 1000 - Double(lo.0)) / Double(hi.0 - lo.0)
                return Marker(lat: lo.1.lat + f * (hi.1.lat - lo.1.lat), lon: lo.1.lon + f * (hi.1.lon - lo.1.lon), how: "interp K\(lo.0)..K\(hi.0)", addr: lo.1.addr, prov: lo.1.prov, conf: "medium", ambiguous: lo.1.ambiguous)
            }
            return nil
        }
        if frac < 0.05 { return Marker(lat: a.lat, lon: a.lon, how: "marker K\(kmi)", addr: a.addr, prov: a.prov, conf: "high", ambiguous: a.ambiguous) }
        for step in [1, 2, 3] {
            if let b = await kmMarker(key, code, kmi + step, region), GeoMath.distanceMeters(a.lat, a.lon, b.lat, b.lon) < 1500 * Double(step) {
                let f = frac / Double(step)
                return Marker(lat: a.lat + f * (b.lat - a.lat), lon: a.lon + f * (b.lon - a.lon), how: String(format: "marker K%d->K%d x%.2f", kmi, kmi + step, frac), addr: a.addr, prov: a.prov, conf: "high", ambiguous: a.ambiguous)
            }
            if let c = await kmMarker(key, code, kmi - step, region), GeoMath.distanceMeters(a.lat, a.lon, c.lat, c.lon) < 1500 * Double(step) {
                let f = frac / Double(step)
                return Marker(lat: a.lat + f * (a.lat - c.lat), lon: a.lon + f * (a.lon - c.lon), how: String(format: "marker K%d extrap K%d x%.2f", kmi, kmi - step, frac), addr: a.addr, prov: a.prov, conf: "high", ambiguous: a.ambiguous)
            }
        }
        return Marker(lat: a.lat, lon: a.lon, how: "marker K\(kmi) (metres dropped)", addr: a.addr, prov: a.prov, conf: "medium", ambiguous: a.ambiguous)
    }

    private static func routeInterp(_ key: String, _ aLat: Double, _ aLon: Double, _ bLat: Double, _ bLon: Double, _ frac: Double, _ gapKm: Double) async -> (Double, Double)? {
        let a = CoordinateConverter.wgs84ToGcj02(.init(latitude: aLat, longitude: aLon))
        let b = CoordinateConverter.wgs84ToGcj02(.init(latitude: bLat, longitude: bLon))
        guard let d = await get(key, "v3/direction/driving", [
            "origin": String(format: "%.6f,%.6f", a.longitude, a.latitude),
            "destination": String(format: "%.6f,%.6f", b.longitude, b.latitude),
            "strategy": "2", "extensions": "base",
        ]), let path = ((d["route"] as? [String: Any])?["paths"] as? [[String: Any]])?.first else { return nil }
        let distKm = Double(path["distance"] as? String ?? "") ?? 0
        guard gapKm > 0, distKm / 1000 / gapKm > 0.8, distKm / 1000 / gapKm < 1.25 else { return nil }
        var pl: [(Double, Double)] = []
        for step in (path["steps"] as? [[String: Any]]) ?? [] {
            for q in ((step["polyline"] as? String) ?? "").split(separator: ";") {
                guard let loc = split(String(q)) else { continue }
                if pl.isEmpty || pl.last! != loc { pl.append(loc) }
            }
        }
        guard pl.count >= 2 else { return nil }
        let seg = (0..<(pl.count - 1)).map { i in GeoMath.distanceMeters(pl[i].1, pl[i].0, pl[i + 1].1, pl[i + 1].0) }
        var target = seg.reduce(0, +) * frac
        for i in seg.indices {
            if target <= seg[i] {
                let u = seg[i] == 0 ? 0 : target / seg[i]
                let lon = pl[i].0 + u * (pl[i + 1].0 - pl[i].0)
                let lat = pl[i].1 + u * (pl[i + 1].1 - pl[i].1)
                let w = CoordinateConverter.gcj02ToWgs84(.init(latitude: lat, longitude: lon))
                return (w.latitude, w.longitude)
            }
            target -= seg[i]
        }
        return nil
    }

    private struct Poi { var score: Int; var name, admin: String; var lat, lon, gcjLon, gcjLat: Double; var far: Bool }

    private static func poi(_ key: String, _ name: String, _ ftype: String, _ provs: [String], _ code: String?, _ roadname: String?) async -> Poi? {
        var base = name.hasSuffix(ftype) ? String(name.dropLast(ftype.count)) : name
        let stripped = base.replacingOccurrences(of: "^.*?(?:段|高速|方向)", with: "", options: .regularExpression)
        if !stripped.isEmpty { base = stripped }
        let syn = ["服务区": ["服务区", "停车区"], "停车区": ["停车区", "服务区"], "站口": ["收费站"], "收费站": ["收费站"]]
        let types = syn[ftype] ?? [ftype]
        let names = types.map { base + $0 }
        let regs = provs.filter { !$0.isEmpty }.sorted { provinces.contains($0) && !provinces.contains($1) ? false : !provinces.contains($0) && provinces.contains($1) }
        var reqs: [(String, String?)] = []
        for pv in regs.prefix(3) { for kw in names { reqs.append((kw, pv)) } }
        for kw in names { reqs.append((kw, nil)) }
        if let roadname, !roadname.isEmpty, let first = names.first { reqs.append((roadname + first, nil)) }
        var cands: [Poi] = []
        for (kw, city) in reqs {
            var q = ["keywords": kw, "offset": "20", "extensions": "base"]
            if let city { q["city"] = city; q["citylimit"] = "true" }
            guard let d = await get(key, "v3/place/text", q), let pois = d["pois"] as? [[String: Any]] else { continue }
            for p in pois {
                let nm = (p["name"] as? String) ?? ""
                let tp = (p["type"] as? String) ?? ""
                let core = nm.replacingOccurrences(of: "[（(].*?[)）]", with: "", options: .regularExpression)
                guard names.contains(where: { core == $0 || core.hasPrefix($0) || (core.hasSuffix($0) && core.count - $0.count <= 3) }) else { continue }
                if ftype == "隧道" && !(tp + nm).contains("隧道") { continue }
                var sc = names.contains(core) ? 10 : 5
                if let code, (nm + ((p["address"] as? String) ?? "")).contains(code) { sc += 5 }
                if let roadname, roadname.count >= 2, nm.contains(String(roadname.prefix(2))) { sc += 3 }
                guard let loc = split(p["location"] as? String ?? "") else { continue }
                let w = CoordinateConverter.gcj02ToWgs84(.init(latitude: loc.1, longitude: loc.0))
                let admin = ((p["pname"] as? String) ?? "") + ((p["cityname"] as? String) ?? "") + ((p["adname"] as? String) ?? "")
                cands.append(Poi(score: sc, name: nm, admin: admin, lat: w.latitude, lon: w.longitude, gcjLon: loc.0, gcjLat: loc.1, far: false))
            }
            if (cands.map(\.score).max() ?? 0) >= 15 { break }
        }
        if cands.isEmpty { return nil }
        var pool = cands
        if provs.contains(where: { !$0.isEmpty }) {
            let inn = cands.filter { c in provs.contains { !$0.isEmpty && c.admin.contains($0) } }.map { var x = $0; x.score += 5; return x }
            if inn.isEmpty { return nil }
            pool = inn
        }
        guard var top = pool.max(by: { $0.score < $1.score }) else { return nil }
        top.far = pool.contains { GeoMath.distanceMeters($0.lat, $0.lon, top.lat, top.lon) > 30000 && $0.score >= top.score - 5 }
        return top
    }

    private static func tunnelStep(_ key: String, _ name: String, _ lon: Double, _ lat: Double, _ side: String) async -> (Double, Double, String)? {
        let pairs = [((-0.08, 0.035), (0.04, -0.015)), ((0.04, -0.015), (-0.08, 0.035))]
        for pair in pairs {
            let o = (lon + pair.0.0, lat + pair.0.1)
            let dest = (lon + pair.1.0, lat + pair.1.1)
            guard let d = await get(key, "v3/direction/driving", [
                "origin": String(format: "%.6f,%.6f", o.0, o.1),
                "destination": String(format: "%.6f,%.6f", dest.0, dest.1),
                "strategy": "2", "extensions": "base",
            ]), let steps = ((((d["route"] as? [String: Any])?["paths"] as? [[String: Any]])?.first)?["steps"] as? [[String: Any]]) else { continue }
            for s in steps where (s["road"] as? String) == name {
                let pts = ((s["polyline"] as? String) ?? "").split(separator: ";").compactMap { split(String($0)) }
                guard pts.count >= 2 else { continue }
                let ends = [pts[0], pts[pts.count - 1]]
                let e = ends.max { sideScore(side, $0.0, $0.1) < sideScore(side, $1.0, $1.1) }!
                let w = CoordinateConverter.gcj02ToWgs84(.init(latitude: e.1, longitude: e.0))
                return (w.latitude, w.longitude, "step")
            }
        }
        return nil
    }

    private static func osmPortal(_ lat: Double, _ lon: Double, _ side: String, _ travel: String?) async -> (Double, Double, String)? {
        let q = "[out:json][timeout:25];way[\"tunnel\"=\"yes\"][\"highway\"~\"motorway|trunk|primary\"](around:6000,\(lat),\(lon));out geom;"
        guard let body = await postForm("https://overpass-api.de/api/interpreter", q),
              let data = body.data(using: .utf8),
              let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let els = json["elements"] as? [[String: Any]] else { return nil }
        struct Way { var geom: [(Double, Double)] }
        var ways: [Way] = []
        for el in els {
            let g = (el["geometry"] as? [[String: Any]]) ?? []
            let pts = g.compactMap { row -> (Double, Double)? in
                guard let la = row["lat"] as? Double, let lo = row["lon"] as? Double else { return nil }
                return (la, lo)
            }
            if pts.count > 1 { ways.append(Way(geom: pts)) }
        }
        if ways.isEmpty { return nil }
        func len(_ w: Way) -> Double { zip(w.geom, w.geom.dropFirst()).reduce(0) { $0 + GeoMath.distanceMeters($1.0.0, $1.0.1, $1.1.0, $1.1.1) } }
        let longest = ways.map(len).max() ?? 0
        var ends: [(Double, Double, String)] = []
        for w in ways where len(w) >= 0.5 * longest && w.geom.count >= 2 {
            ends.append((w.geom[0].0, w.geom[0].1, "start"))
            ends.append((w.geom[w.geom.count - 1].0, w.geom[w.geom.count - 1].1, "end"))
        }
        guard var best = ends.max(by: { sideScore(side, $0.1, $0.0) < sideScore(side, $1.1, $1.0) }) else { return nil }
        if let travel, !travel.isEmpty, let start = ends.first(where: { $0.2 == "start" && GeoMath.distanceMeters($0.0, $0.1, best.0, best.1) < 300 }) {
            best = start
        }
        return (best.0, best.1, "OSM tunnel way \(best.2)")
    }

    private static func sideScore(_ side: String, _ lon: Double, _ lat: Double) -> Double {
        switch side { case "东": return lon; case "西": return -lon; case "北": return lat; case "南": return -lat; default: return 0 }
    }

    private static var cityCache: [String] = []
    private static var citiesLoaded = false

    private static func cities(_ key: String) async -> [String] {
        if citiesLoaded { return cityCache }
        citiesLoaded = true
        guard let d = await get(key, "v3/config/district", ["keywords": "中国", "subdistrict": "2", "extensions": "base"]),
              let root = (d["districts"] as? [[String: Any]])?.first,
              let provs = root["districts"] as? [[String: Any]] else { return cityCache }
        for pv in provs {
            for c in (pv["districts"] as? [[String: Any]]) ?? [] {
                var name = (c["name"] as? String) ?? ""
                for suf in ["自治州", "地区", "市", "盟"] where name.hasSuffix(suf) { name = String(name.dropLast(suf.count)) }
                if name.count >= 2 && !provinces.contains(name) { cityCache.append(name) }
            }
        }
        return cityCache
    }

    private static func regionWords(_ p: TicketParse, _ sig: RegionSignals, _ full: String, _ key: String) async -> [String] {
        var w = sig.provinces.map(\.1)
        if !key.isEmpty { for c in await cities(key) where full.contains(c) || p.norm.contains(c) { w.append(c) } }
        for a in p.admin {
            for piece in all("[\\p{Han}]+?(?:省|自治区|自治州|市|地区|县|区)", a) {
                let core = piece[0].replacingOccurrences(of: "(省|自治区|自治州|市|地区|县|区)$", with: "", options: .regularExpression)
                if core.count >= 2 { w.append(core) }
            }
        }
        var seen = Set<String>()
        return w.filter { seen.insert($0).inserted }
    }

    // MARK: parse helpers

    private struct Found { var km: Double; var metres: Int?; var span: (Int, Int) }

    private static func firstCode(_ t: String) -> String? {
        var search = t.startIndex
        while let r = t.range(of: "[GS]\\d{1,4}", options: .regularExpression, range: search..<t.endIndex) {
            if r.lowerBound > t.startIndex {
                let prev = t[t.index(before: r.lowerBound)]
                if prev.isNumber || ("A"..."Z").contains(prev) { search = r.upperBound; continue }
            }
            if r.upperBound < t.endIndex {
                let next = t[r.upperBound]
                let han = next.unicodeScalars.contains { (0x4e00...0x9fff).contains($0.value) }
                if !han && !next.isWhitespace && next != "K" { search = r.upperBound; continue }
            }
            return String(t[r])
        }
        return nil
    }

    private static func roadNameOf(_ t: String) -> String? {
        guard let m = first("([\\p{Han}]{2,6}?)(?:高速公路|高速|快速路)", t) else { return nil }
        var nm = m[1]
        let stripped = nm.replacingOccurrences(of: "^.*(?:省|市|县|区|州|盟|旗)", with: "", options: .regularExpression)
        if !stripped.isEmpty { nm = stripped }
        return nm
    }

    private static func nameToCode(_ nm: String, _ hint: String?) -> (String, String, String)? {
        if let v = names[nm] { return (v, nm, "exact") }
        if let hit = names.filter({ nm.hasSuffix($0.key) && $0.key.count >= 2 }).max(by: { $0.key.count < $1.key.count }) {
            return (hit.value, hit.key, "suffix")
        }
        let cands = names.filter { edit1($0.key, nm) }
        if let hint, let hit = cands.first(where: { $0.value == hint }) { return (hit.value, hit.key, "ocrfix") }
        if cands.count == 1, let e = cands.first { return (e.value, e.key, "fuzzy") }
        return nil
    }

    private static func edit1(_ a: String, _ b: String) -> Bool {
        let aa = Array(a), bb = Array(b)
        guard aa.count == bb.count else { return false }
        return zip(aa, bb).filter { $0 != $1 }.count <= 1
    }

    private static func stakesIn(_ t: String) -> [Found] {
        var found: [Found] = []
        func add(_ km: Double, _ metres: Int?, _ s: Int, _ e: Int) {
            if found.contains(where: { !(e <= $0.span.0 || s >= $0.span.1) }) { return }
            found.append(Found(km: km, metres: metres, span: (s, e)))
        }
        for m in spans("K(\\d{1,4})\\s*\\+\\s*(\\d{1,4})\\s*(?:M|米)?", t) {
            let me = Int(m.groups[1]) ?? 0
            if me < 1000 { add(Double(m.groups[0]) ?? 0, me, m.start, m.end) }
        }
        for m in spans("(\\d{1,4}(?:\\.\\d+)?)\\s*(?:公里|千米|KM)\\s*\\+?\\s*(\\d{1,4})?\\s*(?:米|M)?", t) {
            if m.start > 0 {
                let prev = Array(t)[m.start - 1]
                if prev.isNumber || prev == "G" || prev == "S" { continue }
            }
            let me = m.groups.count > 1 && !m.groups[1].isEmpty ? Int(m.groups[1]) : nil
            if let me, me >= 1000 { continue }
            add(Double(m.groups[0]) ?? 0, me, m.start, m.end)
        }
        for m in spans("K(\\d{1,4})", t) {
            if m.start > 0 {
                let prev = Array(t)[m.start - 1]
                if prev.isNumber || ("A"..."Z").contains(prev) { continue }
            }
            if m.end < t.count {
                let next = Array(t)[m.end]
                if next.isNumber || next == "+" { continue }
            }
            add(Double(m.groups[0]) ?? 0, nil, m.start, m.end)
        }
        return found.sorted { $0.span.0 < $1.span.0 }
    }

    private static func glued(_ t: String) -> ([RecogGlued], (Int, Int)?) {
        guard let m = spans("([GS])(\\d{3,8})\\s*(?:公里|千米|KM)\\s*(?:(\\d{1,3})\\s*(?:米|M)?)?", t).first else { return ([], nil) }
        let letter = m.groups[0]
        let digits = m.groups[1]
        let metres = m.groups.count > 2 && !m.groups[2].isEmpty ? Int(m.groups[2]) : nil
        var out: [RecogGlued] = []
        for n in 1...4 {
            if n >= digits.count { continue }
            let kmDigits = String(digits.dropFirst(n))
            if kmDigits.count > 1 && kmDigits.hasPrefix("0") { continue }
            guard let km = Int(kmDigits), km > 0, km <= 5000 else { continue }
            out.append(RecogGlued(code: letter + String(digits.prefix(n)), km: Double(km), metres: metres))
        }
        return (out, (m.start, m.end))
    }

    private static func authority(_ t: String, _ p: String) -> Bool {
        guard let idx = t.range(of: p) else { return false }
        var rest = String(t[idx.upperBound...])
        for prefix in ["省", "市", "自治区"] where rest.hasPrefix(prefix) { rest = String(rest.dropFirst(prefix.count)) }
        return rest.hasPrefix("公安") || rest.hasPrefix("交通") || (rest.hasPrefix("高速") && !rest.hasPrefix("高速公路交通安全条例"))
    }

    private static func fold(_ s: String) -> String {
        String(s.map { ch -> Character in
            guard let v = ch.unicodeScalars.first?.value else { return ch }
            if (0xFF01...0xFF5E).contains(v) { return Character(UnicodeScalar(v - 0xFEE0)!) }
            if v == 0x3000 { return " " }
            return ch
        })
    }

    private static func joinBroken(_ s: String) -> String {
        let runes = Array(s)
        var b = ""
        var i = 0
        func left(_ c: Character) -> Bool { c.isNumber || c == "K" || c == "+" || "公千米".contains(c) }
        while i < runes.count {
            let r = runes[i]
            if left(r) {
                var j = i + 1
                while j < runes.count && " \t\n".contains(runes[j]) { j += 1 }
                if j > i + 1, j < runes.count {
                    let next = runes[j]
                    let ok = ((r.isNumber || r == "K" || r == "+") && (next.isNumber || next == "+" || "公千米KM".contains(next)))
                        || (("公千米+".contains(r)) && next.isNumber)
                    if ok { b.append(r); i = j; continue }
                }
            }
            b.append(r)
            i += 1
        }
        return b
    }

    private static func collapseHan(_ s: String) -> String {
        var cur = s
        while let r = cur.range(of: "([\\p{Han}])[ \\t]+([\\p{Han}])", options: .regularExpression) {
            let piece = String(cur[r])
            let chars = piece.filter { !$0.isWhitespace }
            cur.replaceSubrange(r, with: chars)
        }
        return cur
    }

    private static func split(_ s: String) -> (Double, Double)? {
        let p = s.split(separator: ",")
        guard p.count == 2, let lon = Double(p[0]), let lat = Double(p[1]), abs(lat) <= 90, abs(lon) <= 180 else { return nil }
        return (lon, lat)
    }

    private struct Span { var start, end: Int; var groups: [String] }

    private static func spans(_ pattern: String, _ text: String) -> [Span] {
        guard let re = try? NSRegularExpression(pattern: pattern) else { return [] }
        let ns = text as NSString
        return re.matches(in: text, range: NSRange(location: 0, length: ns.length)).map { m in
            var groups: [String] = []
            if m.numberOfRanges > 1 {
                for i in 1..<m.numberOfRanges {
                    groups.append(m.range(at: i).location == NSNotFound ? "" : ns.substring(with: m.range(at: i)))
                }
            }
            return Span(start: m.range.location, end: m.range.location + m.range.length, groups: groups)
        }
    }

    private static func first(_ pattern: String, _ text: String) -> [String]? {
        all(pattern, text).first
    }

    private static func all(_ pattern: String, _ text: String) -> [[String]] {
        guard let re = try? NSRegularExpression(pattern: pattern) else { return [] }
        let ns = text as NSString
        return re.matches(in: text, range: NSRange(location: 0, length: ns.length)).map { m in
            (0..<m.numberOfRanges).map { i in m.range(at: i).location == NSNotFound ? "" : ns.substring(with: m.range(at: i)) }
        }
    }

    private static func slice(_ t: String, _ start: Int, _ end: Int) -> String {
        let chars = Array(t)
        guard start >= 0, end <= chars.count, start <= end else { return "" }
        return String(chars[start..<end])
    }

    private static var httpCache: [String: [String: Any]] = [:]

    private static func get(_ key: String, _ path: String, _ query: [String: String]) async -> [String: Any]? {
        if key.isEmpty { return nil }
        let cacheKey = path + query.keys.sorted().map { "\($0)=\(query[$0] ?? "")" }.joined(separator: "&")
        if let cached = httpCache[cacheKey] { return cached }
        var parts = query.map { "\($0.key)=\($0.value.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed) ?? "")" }
        parts.append("key=\(key.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed) ?? "")")
        guard let url = URL(string: "https://restapi.amap.com/\(path)?\(parts.joined(separator: "&"))") else { return nil }
        var req = URLRequest(url: url)
        req.setValue("XiaoMoto/31", forHTTPHeaderField: "User-Agent")
        guard let (data, _) = try? await URLSession.shared.data(for: req),
              let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return nil }
        if (json["status"] as? String) == "1" {
            httpCache[cacheKey] = json
        }
        return json
    }

    private static func postForm(_ url: String, _ data: String) async -> String? {
        guard let u = URL(string: url) else { return nil }
        var req = URLRequest(url: u)
        req.httpMethod = "POST"
        req.setValue("application/x-www-form-urlencoded", forHTTPHeaderField: "Content-Type")
        req.setValue("XiaoMoto/31", forHTTPHeaderField: "User-Agent")
        let body = "data=\(data.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed) ?? "")"
        req.httpBody = body.data(using: .utf8)
        guard let (raw, _) = try? await URLSession.shared.data(for: req), let text = String(data: raw, encoding: .utf8), text.trimmingCharacters(in: .whitespaces).hasPrefix("{") else { return nil }
        return text
    }
}
