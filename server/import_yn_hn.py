#!/usr/bin/env python3
"""Turn the Yunnan and Hainan camera workbooks into import_points.py JSON.

Coordinates are WGS-84, same as the server. AMap's geocoder is used only when
AMAP_KEY is set (the same "{road} {km}公里" query as server/internal/recog).
This run can also place a stake on OpenStreetMap road geometry, or a named
junction via Nominatim. Nothing here reads or writes an API key.

Rows that are not a single camera (the Yunnan disclaimer block, and the
Hainan penalty-code sheet) are not map points. A camera row that cannot be
placed confidently is written to the unresolved CSV/Markdown and is not
inserted.

Usage:
  python3 import_yn_hn.py \
    --yunnan yunnan.xlsx --hainan hainan.xlsx \
    --existing /tmp/all_points.json \
    --osm /tmp/osm/yn_f.geojson --osm /tmp/osm/hn_f.geojson --osm /tmp/osm/hn_names.geojson \
    --out server/imports/2026-11-yunnan-hainan.json \
    --review server/imports/2026-11-yunnan-hainan-unresolved.md
"""

from __future__ import annotations

import argparse
import csv
import json
import math
import os
import re
import time
import urllib.parse
import urllib.request
import uuid
from collections import defaultdict
from datetime import date, datetime

NS = uuid.UUID("8f3c2a10-6b4e-4d1a-9c77-0a1b2c3d4e5f")
EARTH_M = 6371000.0
JOIN_M = 60.0
SNAP_OK_M = 1500.0
NOMINATIM = "https://nominatim.openstreetmap.org/search"
UA = "xiaomoto-ynhn-import/1.0 (camera workbook import)"
VIEWBOX = {
    "云南": "97.3,29.4,106.3,21.0",
    "海南": "108.5,20.25,111.25,18.05",
}

YN_CODES = {
    "汕昆": "G78",
    "新昆楚大": "G5621",
    "昆楚大": "G5621",
    "杭瑞": "G56",
    "银昆": "G85",
}

CODE_RE = re.compile(r"([GSYX])\s*(\d{1,4})", re.I)
STAKE_PATTERNS = (
    re.compile(r"[Kk]\s*(\d+(?:\.\d+)?)\s*(?:\+|＋)\s*(\d+(?:\.\d+)?)\s*[mM米]?"),
    re.compile(r"(\d+(?:\.\d+)?)\s*公里\s*(\d+(?:\.\d+)?)\s*[mM米]"),
    re.compile(r"(\d+(?:\.\d+)?)\s*公里\s*(?:\+|＋)\s*(\d+(?:\.\d+)?)\s*[mM米]?"),
    re.compile(r"(\d+(?:\.\d+)?)\s*km\s*(?:\+|＋)\s*(\d+(?:\.\d+)?)\s*[mM米]", re.I),
    re.compile(r"[Kk]\s*(\d+(?:\.\d+)?)\s*公里"),
    re.compile(r"(\d+(?:\.\d+)?)\s*km\b", re.I),
)
OFFSET_RE = re.compile(r"(东南|东北|西南|西北|东|南|西|北)\s*(\d+(?:\.\d+)?)\s*[mM米]")
BEARING = {"北": 0, "东北": 45, "东": 90, "东南": 135, "南": 180, "西南": 225, "西": 270, "西北": 315}


def stable_id(*parts):
    return str(uuid.uuid5(NS, "|".join(str(p) for p in parts)))


def haversine(lon1, lat1, lon2, lat2):
    p1, p2 = math.radians(lat1), math.radians(lat2)
    dlat = math.radians(lat2 - lat1)
    dlon = math.radians(lon2 - lon1)
    h = math.sin(dlat / 2) ** 2 + math.cos(p1) * math.cos(p2) * math.sin(dlon / 2) ** 2
    return 2 * EARTH_M * math.asin(min(1.0, math.sqrt(h)))


def heading(lon1, lat1, lon2, lat2):
    dlon = math.radians(lon2 - lon1)
    y = math.sin(dlon) * math.cos(math.radians(lat2))
    x = math.cos(math.radians(lat1)) * math.sin(math.radians(lat2)) - math.sin(
        math.radians(lat1)
    ) * math.cos(math.radians(lat2)) * math.cos(dlon)
    return (math.degrees(math.atan2(y, x)) + 360.0) % 360.0


def angle_diff(a, b):
    d = abs(a - b) % 360.0
    return min(d, 360.0 - d)


def move(lat, lon, bearing_deg, meters):
    rad = math.radians(bearing_deg)
    dlat = meters * math.cos(rad) / 111320.0
    dlon = meters * math.sin(rad) / (111320.0 * max(0.2, math.cos(math.radians(lat))))
    return lat + dlat, lon + dlon


def wgs_to_gcj(lat, lon):
    if not (72.004 <= lon <= 137.8347 and 0.8293 <= lat <= 55.8271):
        return lat, lon
    a, ee = 6378245.0, 0.00669342162296594323

    def tlat(x, y):
        ret = -100.0 + 2.0 * x + 3.0 * y + 0.2 * y * y + 0.1 * x * y + 0.2 * math.sqrt(abs(x))
        ret += (20.0 * math.sin(6.0 * x * math.pi) + 20.0 * math.sin(2.0 * x * math.pi)) * 2.0 / 3.0
        ret += (20.0 * math.sin(y * math.pi) + 40.0 * math.sin(y / 3.0 * math.pi)) * 2.0 / 3.0
        ret += (160.0 * math.sin(y / 12.0 * math.pi) + 320 * math.sin(y * math.pi / 30.0)) * 2.0 / 3.0
        return ret

    def tlon(x, y):
        ret = 300.0 + x + 2.0 * y + 0.1 * x * x + 0.1 * x * y + 0.1 * math.sqrt(abs(x))
        ret += (20.0 * math.sin(6.0 * x * math.pi) + 20.0 * math.sin(2.0 * x * math.pi)) * 2.0 / 3.0
        ret += (20.0 * math.sin(x * math.pi) + 40.0 * math.sin(x / 3.0 * math.pi)) * 2.0 / 3.0
        ret += (150.0 * math.sin(x / 12.0 * math.pi) + 300.0 * math.sin(x / 30.0 * math.pi)) * 2.0 / 3.0
        return ret

    dlat = tlat(lon - 105.0, lat - 35.0)
    dlon = tlon(lon - 105.0, lat - 35.0)
    rad = lat / 180.0 * math.pi
    magic = 1 - ee * math.sin(rad) ** 2
    sqrt_magic = math.sqrt(magic)
    dlat = (dlat * 180.0) / ((a * (1 - ee)) / (magic * sqrt_magic) * math.pi)
    dlon = (dlon * 180.0) / (a / sqrt_magic * math.cos(rad) * math.pi)
    return lat + dlat, lon + dlon


def gcj_text(lat, lon):
    glat, glon = wgs_to_gcj(lat, lon)
    return f"{glon:.6f},{glat:.6f}"


def trim(text, limit):
    chars = list(text or "")
    return "".join(chars[:limit])


def cell(value):
    if value is None:
        return ""
    if isinstance(value, datetime):
        return value.strftime("%Y-%m-%d")
    if isinstance(value, date):
        return value.strftime("%Y-%m-%d")
    return str(value).strip()


def road_code_in(text):
    match = CODE_RE.search(text or "")
    if not match:
        return ""
    return (match.group(1) + match.group(2)).upper()


def parse_stakes(text):
    cleaned = re.sub(r"全长\s*\d+(?:\.\d+)?\s*(?:km|公里)", " ", text or "", flags=re.I)
    occupied = [False] * (len(cleaned) + 1)
    found = []
    for pattern in STAKE_PATTERNS:
        for match in pattern.finditer(cleaned):
            if any(occupied[match.start() : match.end()]):
                continue
            for index in range(match.start(), match.end()):
                occupied[index] = True
            groups = [group for group in match.groups() if group is not None]
            km = float(groups[0])
            metres = float(groups[1]) if len(groups) > 1 else 0.0
            if km > 10000:
                continue
            found.append((match.start(), km + metres / 1000.0))
    found.sort()
    deduped = []
    for _, value in found:
        if deduped and abs(deduped[-1] - value) < 0.0005:
            continue
        deduped.append(value)
    vague = bool(re.search(r"\d+\s*公里处", cleaned)) and deduped and all(
        abs(value - round(value)) < 1e-6 for value in deduped
    )
    return deduped, vague


def stake_label(km):
    whole = int(math.floor(km + 1e-9))
    metres = int(round((km - whole) * 1000))
    if metres == 1000:
        whole += 1
        metres = 0
    if metres == 0:
        return f"K{whole}+000"
    return f"K{whole}+{metres:03d}"


def range_label(a, b):
    if b is None:
        return stake_label(a)
    return stake_label(a) + "–" + stake_label(b)


class Polyline:
    def __init__(self, coords):
        clean = []
        for lon, lat in coords:
            if clean and haversine(clean[-1][0], clean[-1][1], lon, lat) < 1:
                continue
            clean.append((float(lon), float(lat)))
        self.coords = clean
        self.cum = [0.0]
        for (lon1, lat1), (lon2, lat2) in zip(clean, clean[1:]):
            self.cum.append(self.cum[-1] + haversine(lon1, lat1, lon2, lat2))

    @property
    def length(self):
        return self.cum[-1] if self.coords else 0.0

    def project(self, lon, lat):
        best = None
        for i in range(len(self.coords) - 1):
            lon1, lat1 = self.coords[i]
            lon2, lat2 = self.coords[i + 1]
            seg = self.cum[i + 1] - self.cum[i]
            if seg <= 0:
                continue
            # Local metre projection.
            mean = math.radians((lat1 + lat2) / 2)
            kx = 111320.0 * math.cos(mean)
            ky = 111320.0
            abx, aby = (lon2 - lon1) * kx, (lat2 - lat1) * ky
            apx, apy = (lon - lon1) * kx, (lat - lat1) * ky
            ab2 = abx * abx + aby * aby
            t = 0.0 if ab2 == 0 else max(0.0, min(1.0, (apx * abx + apy * aby) / ab2))
            plon = lon1 + (lon2 - lon1) * t
            plat = lat1 + (lat2 - lat1) * t
            dist = haversine(lon, lat, plon, plat)
            along = self.cum[i] + seg * t
            if best is None or dist < best[0]:
                best = (dist, along, plon, plat)
        if best is None and self.coords:
            lon0, lat0 = self.coords[0]
            return haversine(lon, lat, lon0, lat0), 0.0, lon0, lat0
        return best

    def point_at(self, along):
        if not self.coords:
            return None
        along = max(0.0, min(self.length, along))
        for i in range(len(self.cum) - 1):
            if self.cum[i + 1] >= along or i == len(self.cum) - 2:
                span = self.cum[i + 1] - self.cum[i]
                t = 0.0 if span <= 0 else (along - self.cum[i]) / span
                lon1, lat1 = self.coords[i]
                lon2, lat2 = self.coords[min(i + 1, len(self.coords) - 1)]
                return lon1 + (lon2 - lon1) * t, lat1 + (lat2 - lat1) * t
        return self.coords[-1]


def chain_heading_at_end(coords, at_start):
    if len(coords) < 2:
        return 0.0
    if at_start:
        return heading(coords[1][0], coords[1][1], coords[0][0], coords[0][1])
    return heading(coords[-2][0], coords[-2][1], coords[-1][0], coords[-1][1])


def stitch(segments):
    unused = set(range(len(segments)))
    chains = []
    while unused:
        index = unused.pop()
        chain = list(segments[index])
        grew = True
        while grew:
            grew = False
            end_h = chain_heading_at_end(chain, False)
            start_h = chain_heading_at_end(chain, True)
            for other in list(unused):
                seg = segments[other]
                if len(seg) < 2 or len(chain) < 2:
                    continue
                options = (
                    (chain[-1], seg[0], heading(seg[0][0], seg[0][1], seg[1][0], seg[1][1]), "append"),
                    (chain[-1], seg[-1], heading(seg[-1][0], seg[-1][1], seg[-2][0], seg[-2][1]), "append_rev"),
                    (chain[0], seg[-1], heading(seg[-2][0], seg[-2][1], seg[-1][0], seg[-1][1]), "prepend"),
                    (chain[0], seg[0], heading(seg[1][0], seg[1][1], seg[0][0], seg[0][1]), "prepend_rev"),
                )
                # The heading stored above is the arrival heading at the joint for append,
                # and the departure heading for prepend. Compare with the chain side.
                cands = []
                dist = haversine(chain[-1][0], chain[-1][1], seg[0][0], seg[0][1])
                h = heading(seg[0][0], seg[0][1], seg[1][0], seg[1][1])
                cands.append((dist, angle_diff(end_h, h), "append"))
                dist = haversine(chain[-1][0], chain[-1][1], seg[-1][0], seg[-1][1])
                h = heading(seg[-1][0], seg[-1][1], seg[-2][0], seg[-2][1])
                cands.append((dist, angle_diff(end_h, h), "append_rev"))
                dist = haversine(chain[0][0], chain[0][1], seg[-1][0], seg[-1][1])
                h = heading(seg[-2][0], seg[-2][1], seg[-1][0], seg[-1][1])
                cands.append((dist, angle_diff(start_h, (h + 180) % 360), "prepend"))
                dist = haversine(chain[0][0], chain[0][1], seg[0][0], seg[0][1])
                h = heading(seg[1][0], seg[1][1], seg[0][0], seg[0][1])
                cands.append((dist, angle_diff(start_h, (h + 180) % 360), "prepend_rev"))
                dist, turn, how = min(cands, key=lambda item: (item[0], item[1]))
                if dist > JOIN_M or turn > 55:
                    continue
                if how == "append":
                    chain.extend(seg[1:])
                elif how == "append_rev":
                    chain.extend(list(reversed(seg))[1:])
                elif how == "prepend":
                    chain = list(seg[:-1]) + chain
                else:
                    chain = list(reversed(seg))[1:] + chain
                unused.remove(other)
                grew = True
                break
        chains.append(Polyline(chain))
    return chains


class RoadNet:
    def __init__(self):
        self.by_key = defaultdict(list)

    def add_feature(self, feature):
        geom = feature.get("geometry") or {}
        if geom.get("type") != "LineString":
            return
        props = feature.get("properties") or {}
        highway = props.get("highway") or ""
        if highway not in ("motorway", "trunk", "primary", "secondary"):
            return
        coords = geom.get("coordinates") or []
        if len(coords) < 2:
            return
        refs = re.split(r"[;/,\s]+", str(props.get("ref") or ""))
        name = str(props.get("name") or "")
        keys = set()
        for ref in refs:
            ref = ref.strip().upper()
            if re.fullmatch(r"[GSYX]\d{1,4}", ref):
                keys.add("ref:" + ref)
        if "环岛旅游公路" in name and "连接线" not in name:
            keys.add("name:环岛旅游公路")
        for token in ("棋子湾大道", "洋浦大桥", "椰海大道", "昌江大道", "人民北路", "环城东一路", "育新路", "海垦路"):
            if token in name:
                keys.add("name:" + token)
        for key in keys:
            self.by_key[key].append([(c[0], c[1]) for c in coords])

    def chains(self, key):
        cached = getattr(self, "_chains", None)
        if cached is None:
            self._chains = {}
            cached = self._chains
        if key in cached:
            return cached[key]
        segments = self.by_key.get(key) or []
        if not segments:
            cached[key] = []
            return []
        chains = stitch(segments)
        chains = [c for c in chains if c.length >= 800]
        chains.sort(key=lambda c: c.length, reverse=True)
        cached[key] = chains
        print(f"road {key} segments {len(segments)} chains {len(chains)} longest_km {chains[0].length/1000:.1f}" if chains else f"road {key} empty")
        return chains

    def snap(self, key, lon, lat):
        best = None
        for chain in self.chains(key)[:8]:
            dist, along, plon, plat = chain.project(lon, lat)
            if best is None or dist < best[0]:
                best = (dist, along, plon, plat, chain)
        return best


def load_osm(paths):
    net = RoadNet()
    milestones = []
    for path in paths:
        with open(path, encoding="utf-8") as handle:
            doc = json.load(handle)
        for feature in doc.get("features") or []:
            props = feature.get("properties") or {}
            if props.get("highway") == "milestone":
                geom = feature.get("geometry") or {}
                if geom.get("type") == "Point":
                    lon, lat = geom["coordinates"][:2]
                    try:
                        distance = float(props.get("distance"))
                    except (TypeError, ValueError):
                        distance = None
                    milestones.append({"lon": lon, "lat": lat, "km": distance, "ref": props.get("ref") or ""})
            else:
                net.add_feature(feature)
    return net, milestones


def load_existing(path):
    with open(path, encoding="utf-8") as handle:
        body = json.load(handle)
    points = body["points"] if isinstance(body, dict) else body
    parsed = []
    for point in points:
        # The stored coordinate belongs to one stake. A range in `stake` is the
        # start of that range; notes often repeat both ends and must not be used.
        stake_text = str(point.get("stake") or "")
        name_text = str(point.get("name") or "")
        from_stake, _ = parse_stakes(stake_text)
        from_name, _ = parse_stakes(name_text)
        if from_stake:
            stakes = [from_stake[0]]
        elif len(from_name) == 1:
            stakes = from_name
        else:
            stakes = []
        parsed.append(
            {
                "id": point["id"],
                "name": point.get("name") or "",
                "road": (point.get("road_code") or "").upper(),
                "lat": point["latitude"],
                "lon": point["longitude"],
                "stake": point.get("stake") or "",
                "notes": point.get("notes") or "",
                "direction": point.get("direction") or "",
                "violation": point.get("violation") or "",
                "stakes": stakes,
            }
        )
    return parsed


def nominatim(query, cache, sleep, viewbox=""):
    key = query.strip() + "|" + viewbox
    if key in cache:
        return cache[key]
    params = {
        "format": "jsonv2",
        "limit": "5",
        "countrycodes": "cn",
        "addressdetails": "1",
        "q": query.strip(),
    }
    if viewbox:
        params["viewbox"] = viewbox
        params["bounded"] = "1"
    params = urllib.parse.urlencode(params)
    req = urllib.request.Request(NOMINATIM + "?" + params, headers={"User-Agent": UA})
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            data = json.loads(resp.read().decode("utf-8"))
    except Exception as exc:  # noqa: BLE001 - one failed lookup should not drop the import
        cache[key] = {"error": str(exc)}
        time.sleep(sleep)
        return cache[key]
    cache[key] = data
    time.sleep(sleep)
    return data


def pick_place(results, tokens, province, county):
    if not isinstance(results, list):
        return None, "地名检索失败"
    scored = []
    for result in results:
        disp = result.get("display_name") or ""
        name = result.get("name") or ""
        if province not in disp:
            continue
        if county and county not in disp and county not in name:
            continue
        hits = sum(1 for token in tokens if token and (token in name or token in disp))
        if hits <= 0:
            continue
        try:
            lat = float(result["lat"])
            lon = float(result["lon"])
        except (KeyError, TypeError, ValueError):
            continue
        kind = (result.get("category"), result.get("type"))
        bonus = 0
        if kind[0] in ("highway", "amenity", "man_made", "building"):
            bonus += 2
        if kind[1] in ("bridge", "motorway", "trunk", "primary", "checkpoint", "junction"):
            bonus += 2
        if kind[0] == "place" and kind[1] in ("city", "town", "county"):
            bonus -= 2
        label = name or disp.split(",")[0]
        if not any(token and token in label for token in tokens):
            continue
        scored.append((hits, bonus, lat, lon, label, disp, kind))
    if not scored:
        return None, "地名没有落在指定市县，或结果名称对不上"
    scored.sort(key=lambda item: (item[0], item[1]), reverse=True)
    best = scored[0]
    for other in scored[1:]:
        if other[0] == best[0] and haversine(best[3], best[2], other[3], other[2]) > 2500:
            return None, "同名地点相距超过2.5公里，无法区分（{} / {}）".format(best[4], other[4])
    return {"lat": best[2], "lon": best[3], "name": best[4], "display": best[5]}, ""


def amap_km(key, code, km, province):
    if not key or not code:
        return None
    address = "{} {}公里".format(code, int(km))
    params = urllib.parse.urlencode({"key": key, "address": address})
    req = urllib.request.Request(
        "https://restapi.amap.com/v3/geocode/geo?" + params,
        headers={"User-Agent": UA},
    )
    try:
        with urllib.request.urlopen(req, timeout=20) as resp:
            data = json.loads(resp.read().decode("utf-8"))
    except Exception:
        return None
    if str(data.get("status")) != "1":
        return None
    needle = re.compile(r"(?:^|[^\d]){}公里".format(int(km)))
    hits = []
    for row in data.get("geocodes") or []:
        formatted = str(row.get("formatted_address") or "")
        if code not in formatted or not needle.search(formatted):
            continue
        if province and province not in formatted and province not in str(row.get("province") or ""):
            continue
        loc = str(row.get("location") or "")
        if "," not in loc:
            continue
        glon, glat = loc.split(",", 1)
        # AMap returns GCJ-02. Store WGS-84; the note keeps the GCJ pair.
        wlat, wlon = gcj_to_wgs(float(glat), float(glon))
        hits.append((wlon, wlat, formatted))
    if len(hits) != 1:
        # Several provinces share a code. More than one remaining hit is ambiguous.
        clusters = []
        for hit in hits:
            placed = False
            for group in clusters:
                if haversine(group[0][0], group[0][1], hit[0], hit[1]) < 20000:
                    group.append(hit)
                    placed = True
                    break
            if not placed:
                clusters.append([hit])
        if len(clusters) != 1:
            return None
        hits = clusters[0]
    lon = sum(h[0] for h in hits) / len(hits)
    lat = sum(h[1] for h in hits) / len(hits)
    return {"lon": lon, "lat": lat, "how": "高德地理编码 " + hits[0][2], "error_m": 400}


def gcj_to_wgs(lat, lon):
    if not (72.004 <= lon <= 137.8347 and 0.8293 <= lat <= 55.8271):
        return lat, lon
    g_lat, g_lon = lat, lon
    for _ in range(12):
        f_lat, f_lon = wgs_to_gcj(g_lat, g_lon)
        g_lat += lat - f_lat
        g_lon += lon - f_lon
    return g_lat, g_lon


def official_phrase(device, source):
    official = any(word in (source or "") for word in ("公告", "通告", "公示"))
    if not official:
        return "来源：" + (source or "未注明")
    if any(word in (device or "") for word in ("测速", "区间")):
        return "有官方测速公告 — " + source
    return "有官方公告 — " + source


def read_rows(yunnan_path, hainan_path):
    import openpyxl

    rows = []
    book = openpyxl.load_workbook(yunnan_path, data_only=True)
    sheet = book["Sheet1"]
    for index, raw in enumerate(sheet.iter_rows(values_only=True), 1):
        if index == 1:
            continue
        road, device, stake, direction, speed, action, source = (list(raw) + [None] * 7)[:7]
        road, device, stake = cell(road), cell(device), cell(stake)
        direction, speed, action, source = cell(direction), cell(speed), cell(action), cell(source)
        if not any((road, device, stake, direction, speed, action, source)):
            continue
        if not device or road.startswith("通用规则") or re.match(r"^\d+\.", road):
            rows.append({"kind": "note", "sheet": "云南Sheet1", "row": index, "text": road or device})
            continue
        stakes, vague = parse_stakes(stake)
        code = road_code_in(road)
        name = re.sub(r"\([GSYX]\d{1,4}\)", "", road).strip()
        if not code:
            for token, mapped in YN_CODES.items():
                if token in road:
                    code = mapped
                    break
        rows.append(
            {
                "kind": "camera",
                "sheet": "云南Sheet1",
                "row": index,
                "province": "云南",
                "county": "",
                "road_code": code,
                "road_name": name,
                "device": device,
                "stakes": stakes,
                "vague": vague,
                "direction": "" if direction in ("", "—", "-") else direction,
                "speed": speed,
                "action": action,
                "source": source,
                "place": "",
                "offset_bearing": None,
                "offset_m": None,
                "raw": " ".join(part for part in (road, stake, direction) if part),
                "douyin": "抖音" in source or "视频" in source,
                "note": "",
            }
        )
    book = openpyxl.load_workbook(hainan_path, data_only=True)
    sheet = book["抓拍点位表"]
    for index, raw in enumerate(sheet.iter_rows(values_only=True), 1):
        if index == 1:
            continue
        seq, county, location, device, action, speed, since, source, note = (list(raw) + [None] * 9)[:9]
        location, device = cell(location), cell(device)
        if not location and not device:
            continue
        county, action, speed = cell(county), cell(action), cell(speed)
        since, source, note = cell(since), cell(source), cell(note)
        stakes, vague = parse_stakes(location)
        code = road_code_in(location)
        place, direction, bearing, metres = split_place(location)
        road_name = road_name_of(location, code)
        rows.append(
            {
                "kind": "camera",
                "sheet": "海南抓拍点位表",
                "row": index,
                "seq": cell(seq),
                "province": "海南",
                "county": county,
                "road_code": code,
                "road_name": road_name,
                "device": device,
                "stakes": stakes,
                "vague": vague,
                "direction": direction,
                "speed": speed,
                "action": action,
                "source": source,
                "place": place,
                "offset_bearing": bearing,
                "offset_m": metres,
                "raw": location,
                "douyin": False,
                "note": note,
                "since": since,
                "zone": zone_reason(location),
            }
        )
    return rows


def split_place(location):
    direction = ""
    place = ""
    bearing = None
    metres = None
    match = re.search(r"（([^）]+)）", location)
    if match:
        inner = match.group(1)
        if any(token in inner for token in ("往", "向", "→", "至")) and "km" not in inner.lower():
            direction = inner
        else:
            off = OFFSET_RE.search(inner)
            if off:
                bearing = BEARING[off.group(1)]
                metres = float(off.group(2))
                place = inner[: off.start()].strip(" ，,")
            elif not re.search(r"全长|区间", inner):
                place = inner.strip()
    if not place:
        tail = re.sub(r"（[^）]*）", "", location)
        tail = CODE_RE.sub(" ", tail)
        for pattern in STAKE_PATTERNS:
            tail = pattern.sub(" ", tail)
        for word in ("环岛旅游公路", "旅游公路", "高速", "省道", "国道", "县道", "专道", "专线", "环岛", "公路"):
            tail = tail.replace(word, " ")
        tail = re.sub(r"[Kk]\d+", " ", tail)
        tail = re.sub(r"\s+", "", tail)
        tail = tail.strip(" ：:，,、-~/～")
        if any(token in tail for token in ("路口", "大桥", "路段", "大道", "村", "卡口", "镇", "收费", "服务区", "基地")) or tail.endswith("桥"):
            place = tail
    return place, direction, bearing, metres


def road_name_of(location, code):
    if "环岛旅游公路" in location:
        return "环岛旅游公路"
    if "棋子湾" in location:
        return "棋子湾旅游专道"
    if "核电" in location:
        return "核电专道"
    known = {
        "G98": "环岛高速",
        "G9811": "海三高速",
        "G9813": "万洋高速",
        "G223": "海榆东线",
        "G225": "海榆西线",
        "S213": "省道",
        "S217": "省道",
        "S309": "省道",
        "X231": "县道",
        "X227": "县道",
    }
    if code in known:
        return known[code]
    return ""


def zone_reason(location):
    if "闭合" in location or "全域" in location or "一批" in location or "11处" in location:
        return "这一行是一片区域或一批路口，表里没有单个坐标，不能标成一个抓拍点"
    if "路段" in location and "桩" not in location and not parse_stakes(location)[0]:
        if location.count("-") or "至" in location:
            return ""
    return ""


def road_key(row):
    if row["road_code"]:
        return "ref:" + row["road_code"]
    blob = (row.get("road_name") or "") + row.get("raw", "")
    if "环岛旅游公路" in blob:
        return "name:环岛旅游公路"
    if "棋子湾" in blob:
        return "name:棋子湾大道"
    for token in ("洋浦大桥", "椰海大道", "昌江大道", "人民北路", "环城东一路", "育新路", "海垦路"):
        if token in blob:
            return "name:" + token
    return ""



def search_terms(place):
    text = place or ""
    for suffix in ("卡口", "路口", "交叉口", "路段", "收费站", "服务区", "警务室", "检查站", "执法站"):
        text = text.replace(suffix, " ")
    parts = [part for part in re.split(r"[\s、，,与和及\-]+", text) if len(part) >= 2]
    generic = {"旅游", "高速", "公路", "往", "方向", "镇路"}
    out = []
    for part in parts:
        if part in generic or len(part) < 2 or part in out:
            continue
        out.append(part)
    return out[:6]


def tokens_of(place):
    terms = search_terms(place)
    return terms or [place]


def build(rows, net, existing, cache, sleep, amap_key):
    anchors = defaultdict(list)
    for point in existing:
        if not point["stakes"] or not point["road"]:
            continue
        # Yunnan G56 markers live around latitude 25. A Zhejiang G56 marker must not calibrate Yunnan.
        if point["road"] == "G56" and not (21.0 <= point["lat"] <= 29.5 and 97.0 <= point["lon"] <= 106.5):
            continue
        key = "ref:" + point["road"]
        for stake in point["stakes"]:
            anchors[key].append((stake, point["lon"], point["lat"], "已有点 " + point["id"][:8]))

    # Place-name lookups that can also become anchors when a stake is present.
    located_places = {}
    for row in rows:
        if row["kind"] != "camera" or not row.get("place") or row.get("zone"):
            continue
        viewbox = VIEWBOX.get(row["province"], "")
        queries = []
        for term in search_terms(row["place"]):
            queries.append(" ".join(part for part in (term, row.get("county"), row["province"]) if part))
            if len(term) <= 3 and not term.endswith("镇"):
                queries.append(" ".join(part for part in (term + "镇", row.get("county"), row["province"]) if part))
        picked, reason = None, "地名没有唯一结果"
        seen = set()
        for query in queries:
            if not query or query in seen:
                continue
            seen.add(query)
            results = nominatim(query, cache, sleep, viewbox)
            term = query.split()[0]
            picked, reason = pick_place(results, [term], row["province"], row.get("county") or "")
            if picked:
                break
        located_places[id(row)] = (picked, reason)
        if picked and row["stakes"] and len(row["stakes"]) == 1 and not row["vague"]:
            key = road_key(row)
            snap = net.snap(key, picked["lon"], picked["lat"]) if key else None
            if snap and snap[0] <= SNAP_OK_M:
                anchors[key].append((row["stakes"][0], snap[2], snap[3], row["place"]))

    anchor_points = {}
    for key, items in anchors.items():
        uniq = {}
        for stake, lon, lat, _label in items:
            uniq.setdefault(round(stake, 3), (stake, lon, lat))
        anchor_points[key] = list(uniq.values())
        if len(uniq) >= 2:
            print(f"anchors {key} {len(uniq)}")

    points = []
    updates = []
    review = []
    counts = {
        "camera_rows": 0,
        "note_rows": 0,
        "added": 0,
        "updated": 0,
        "skipped_duplicate": 0,
        "unresolved_rows": 0,
    }
    for row in rows:
        if row["kind"] == "note":
            counts["note_rows"] += 1
            continue
        counts["camera_rows"] += 1
        outcome = place_row(row, net, existing, located_places.get(id(row)), anchor_points, amap_key)
        if outcome.get("skipped"):
            counts["skipped_duplicate"] += 1
        if outcome["updates"]:
            updates.extend(outcome["updates"])
            counts["updated"] += len(outcome["updates"])
        if outcome["points"]:
            points.extend(outcome["points"])
            counts["added"] += len(outcome["points"])
        if outcome["review"]:
            review.append(outcome["review"])
            if not outcome["points"] and not outcome["updates"]:
                counts["unresolved_rows"] += 1
            elif outcome["review"].get("partial"):
                counts["unresolved_rows"] += 1
    points, collided = drop_stake_collisions(points)
    seen_drop = set()
    for point, other in collided:
        if point["id"] in seen_drop:
            continue
        seen_drop.add(point["id"])
        counts["added"] -= 1
        counts["unresolved_rows"] += 1
        review.append(
            {
                "sheet": "生成结果",
                "row": "",
                "province": point.get("province") or "",
                "county": point.get("city") or "",
                "road_code": point.get("road_code") or "",
                "road_name": point.get("road_name") or "",
                "location": point.get("name") or "",
                "device": "",
                "direction": point.get("direction") or "",
                "speed": "",
                "source": "",
                "reason": "和「{}」落到了400米内，但桩号差超过400米，两根都先不标".format(other.get("name")),
                "partial": False,
                "guess_lat": point.get("latitude"),
                "guess_lon": point.get("longitude"),
                "guess_note": "未采用",
            }
        )
    return points, updates, review, counts


def first_stake_km(point):
    stakes, _ = parse_stakes(point.get("stake") or "")
    return stakes[0] if stakes else None


def drop_stake_collisions(points):
    drop = set()
    pairs = []
    for index, point in enumerate(points):
        stake = first_stake_km(point)
        if stake is None:
            continue
        for other in points[index + 1 :]:
            other_stake = first_stake_km(other)
            if other_stake is None or abs(stake - other_stake) <= 0.4:
                continue
            dist = haversine(point["longitude"], point["latitude"], other["longitude"], other["latitude"])
            if dist < 400:
                drop.add(point["id"])
                drop.add(other["id"])
                pairs.append((point, other))
                pairs.append((other, point))
    kept = [point for point in points if point["id"] not in drop]
    return kept, pairs


def matching_existing(existing, row):
    if not row["stakes"] or not row["road_code"]:
        return []
    hits = []
    for point in existing:
        if point["road"] != row["road_code"]:
            continue
        if not point["stakes"]:
            continue
        if len(row["stakes"]) == 1:
            if any(abs(stake - row["stakes"][0]) <= 0.30 for stake in point["stakes"]):
                hits.append(point)
            continue
        ends = row["stakes"][:2]
        if len(point["stakes"]) >= 2:
            other = point["stakes"][:2]
            paired = (
                abs(ends[0] - other[0]) <= 0.5 and abs(ends[1] - other[1]) <= 0.5
            ) or (abs(ends[0] - other[1]) <= 0.5 and abs(ends[1] - other[0]) <= 0.5)
            if paired:
                hits.append(point)
                continue
        if any(min(abs(stake - end) for end in ends) <= 0.30 for stake in point["stakes"]):
            hits.append(point)
    return hits


def place_row(row, net, existing, place_hit, anchors, amap_key):
    if row.get("zone"):
        return {"points": [], "updates": [], "review": review_item(row, row["zone"], None)}
    if row.get("douyin") or (row["stakes"] == [] and "待补充" in row["raw"]):
        return {
            "points": [],
            "updates": [],
            "review": review_item(row, "没有官方桩号。来源不是公告，不能当成抓拍点标到地图上", None),
        }
    dupes = matching_existing(existing, row)
    if dupes and not row["vague"]:
        phrase = official_phrase(row["device"], row["source"])
        updates = []
        for point in dupes:
            if phrase in (point["notes"] or ""):
                continue
            update = {
                "id": point["id"],
                "direction": row["direction"],
                "append_notes": evidence_block(row),
            }
            if not str(point["stake"] or "").strip() and point["stakes"]:
                update["stake"] = stake_label(point["stakes"][0])
            updates.append(update)
        return {"points": [], "updates": updates, "review": None, "skipped": True}

    if row["vague"] and not row.get("place"):
        return {
            "points": [],
            "updates": [],
            "review": review_item(row, "桩号只写到整公里（公里处），没有米数，也没有能核对的地名", None),
        }

    ends = []
    problems = []
    stakes = row["stakes"][:2]
    if not stakes and row.get("place"):
        placed = place_only(row, net, place_hit)
        if placed.get("ok"):
            ends.append(placed)
        else:
            problems.append(placed.get("reason") or "地名没定位到")
            guess = placed.get("guess")
            return {"points": [], "updates": [], "review": review_item(row, "；".join(problems), guess)}
    elif stakes:
        for stake in stakes:
            placed = place_stake(row, stake, net, place_hit if len(stakes) == 1 else None, anchors, amap_key)
            if placed.get("ok"):
                ends.append(placed)
            else:
                problems.append(stake_label(stake) + "：" + (placed.get("reason") or "未定位"))
                if placed.get("guess") and len(stakes) == 1:
                    return {"points": [], "updates": [], "review": review_item(row, "；".join(problems), placed["guess"])}
        if problems and len(ends) != len(stakes):
            guess = ends[0] if ends else None
            return {
                "points": [],
                "updates": [],
                "review": review_item(row, "；".join(problems), guess, partial=bool(ends)),
            }
    else:
        return {
            "points": [],
            "updates": [],
            "review": review_item(row, "没有桩号，也没有能检索的地名", None),
        }

    made = []
    if len(ends) == 2:
        made.extend(interval_points(row, ends))
    elif len(ends) == 1:
        made.append(single_point(row, ends[0], stakes[0] if stakes else None))
    return {"points": made, "updates": [], "review": None}


def named_key(label):
    for token in ("人民北路", "环城东一路", "昌江大道", "育新路", "海垦路", "椰海大道", "棋子湾大道", "洋浦大桥"):
        if token in (label or ""):
            return "name:" + token
    return ""


def road_junction(net, left, right):
    key_a, key_b = named_key(left), named_key(right)
    if not key_a or not key_b:
        return None
    best = None
    for chain in net.chains(key_a)[:4]:
        for other in net.chains(key_b)[:4]:
            for lon, lat in chain.coords:
                dist, _along, plon, plat = other.project(lon, lat)
                if best is None or dist < best[0]:
                    best = (dist, plon, plat)
    if best and best[0] < 80:
        return best[1], best[2]
    return None


def place_only(row, net, place_hit):
    # Bridge north/south from the drawn bridge, when OSM has it.
    if "洋浦大桥" in row["raw"]:
        bridge = bridge_end(net, "name:洋浦大桥", "南" in row["raw"])
        if bridge:
            return {"ok": True, "lat": bridge[1], "lon": bridge[0], "error_m": 150, "how": "OSM洋浦大桥两端", "name": row["place"] or row["raw"]}
    place = row.get("place") or ""
    if "路段" in place and "-" in place and not row["stakes"]:
        picked = place_hit[0] if place_hit else None
        guess = None
        if picked:
            guess = {"lat": picked["lat"], "lon": picked["lon"], "how": "只对上了路段里的「{}」".format(picked["name"])}
        return {"ok": False, "reason": "这是一段路，表里没有桩号，不能收成一个点", "guess": guess}
    if "与" in place and not row["stakes"]:
        left, right = place.split("与", 1)
        hit = road_junction(net, left, right)
        if hit:
            return {
                "ok": True,
                "lat": hit[1],
                "lon": hit[0],
                "error_m": 80,
                "how": "OSM上{}与{}的交点".format(left.strip(), right.strip()),
                "name": place,
            }
        return {"ok": False, "reason": "图上没有同时找到这两条路的交点"}
    picked, reason = place_hit if place_hit else (None, "没有地名")
    if not picked:
        return {"ok": False, "reason": reason or "地名没有唯一结果"}
    lat, lon = picked["lat"], picked["lon"]
    how = "地名「{}」".format(picked["name"])
    if row.get("offset_bearing") is not None and row.get("offset_m"):
        lat, lon = move(lat, lon, row["offset_bearing"], row["offset_m"])
        how += "，再按表内方位偏移{:.0f}米".format(row["offset_m"])
    key = road_key(row)
    needs_road = bool(row["stakes"]) or "路口" in place or "卡口" in place
    if key and needs_road:
        snap = net.snap(key, lon, lat)
        if snap and snap[0] <= SNAP_OK_M:
            lon, lat = snap[2], snap[3]
            how += "，贴到{}上（偏离{:.0f}米）".format(row["road_code"] or row["road_name"] or "道路", snap[0])
        else:
            dist = "{:.0f}米".format(snap[0]) if snap else "没有这条路的线"
            return {
                "ok": False,
                "reason": "地名距{} {}，超过1.5公里或路网里没有这条路，不敢当成桩号点".format(
                    row["road_code"] or row["road_name"] or "道路", dist
                ),
                "guess": {"lat": lat, "lon": lon, "how": how},
            }
    elif row["stakes"] and not key:
        return {
            "ok": False,
            "reason": "有桩号，但路网里没有这条路，不能只用镇名代替",
            "guess": {"lat": lat, "lon": lon, "how": how},
        }
    return {"ok": True, "lat": lat, "lon": lon, "error_m": 400, "how": how + "（Nominatim，WGS-84）", "name": picked["name"]}


def bridge_end(net, key, south):
    chains = net.chains(key)
    if not chains:
        return None
    chain = chains[0]
    if chain.length > 4000 or chain.length < 30:
        return None
    a, b = chain.coords[0], chain.coords[-1]
    if south:
        return a if a[1] <= b[1] else b
    return a if a[1] >= b[1] else b


def locate_from_anchors(net, key, anchors, stake):
    """Place a stake only when two checked markers sit on the same OSM line and the scale is ~1."""
    if not key or len(anchors) < 2:
        return None
    best = None
    for chain in net.chains(key)[:8]:
        snapped = []
        for item in anchors:
            dist, along, _plon, _plat = chain.project(item[1], item[2])
            if dist <= 800:
                snapped.append((item[0], along, dist))
        snapped.sort()
        uniq = []
        for item in snapped:
            if uniq and abs(uniq[-1][0] - item[0]) < 0.05:
                if item[2] < uniq[-1][2]:
                    uniq[-1] = item
                continue
            uniq.append(item)
        hit_inside = False
        pairs = []
        for left, right in zip(uniq, uniq[1:]):
            delta_stake = right[0] - left[0]
            delta_along = right[1] - left[1]
            if abs(delta_stake) < 0.5:
                continue
            scale = (delta_along / 1000.0) / delta_stake
            # The line may be digitized against the stake direction, so the scale is negative.
            if 0.97 <= abs(scale) <= 1.03:
                pairs.append((left, right, scale))
        if not pairs:
            continue
        for left, right, scale in pairs:
            lo, hi = (left[0], right[0]) if left[0] <= right[0] else (right[0], left[0])
            if lo - 0.05 <= stake <= hi + 0.05:
                along = left[1] + (stake - left[0]) / (right[0] - left[0]) * (right[1] - left[1])
                point = chain.point_at(along)
                if point is None:
                    continue
                error_m = abs(1 - scale) * min(abs(stake - left[0]), abs(stake - right[0])) * 1000 + 200
                candidate = {
                    "ok": True,
                    "lon": point[0],
                    "lat": point[1],
                    "error_m": error_m,
                    "how": "OSM道路几何，夹在已核对桩号之间（比例{:.3f}）".format(scale),
                }
                if best is None or candidate["error_m"] < best["error_m"]:
                    best = candidate
                    hit_inside = True
        if hit_inside:
            continue
        nearest = None
        for left, right, scale in pairs:
            for end in (left, right):
                gap = abs(stake - end[0])
                if gap <= 15 and (nearest is None or gap < nearest[0]):
                    nearest = (gap, end, left, right, scale)
        if nearest is None:
            continue
        gap, end, left, right, scale = nearest
        along = end[1] + (stake - end[0]) / (right[0] - left[0]) * (right[1] - left[1])
        if along < -1000 or along > chain.length + 1000:
            continue
        point = chain.point_at(max(0.0, min(chain.length, along)))
        if point is None:
            continue
        error_m = gap * 1000 * 0.05 + 300
        if error_m > 1500:
            continue
        candidate = {
            "ok": True,
            "lon": point[0],
            "lat": point[1],
            "error_m": error_m,
            "how": "OSM道路几何，从已核对桩号沿路线外推{:.1f}公里".format(gap),
        }
        if best is None or candidate["error_m"] < best["error_m"]:
            best = candidate
    return best


def place_stake(row, stake, net, place_hit, anchors, amap_key):
    place_fail = None
    if place_hit and place_hit[0] and len(row["stakes"]) == 1:
        placed = place_only(row, net, place_hit)
        if placed.get("ok"):
            placed["stake"] = stake
            return placed
        place_fail = placed
        if "超过" in (placed.get("reason") or "") or "一段路" in (placed.get("reason") or ""):
            return placed
    amap = amap_km(amap_key, row["road_code"], stake, row["province"])
    if amap:
        amap["ok"] = True
        amap["stake"] = stake
        return amap
    key = road_key(row)
    located = locate_from_anchors(net, key, anchors.get(key) or [], stake)
    if located:
        located["stake"] = stake
        return located
    if place_fail:
        reason = place_fail.get("reason") or "地名没贴到路上"
        if "标定" not in reason:
            reason += "；附近也没有第二根已核对的桩号能把这个点推出来"
        place_fail["reason"] = reason
        return place_fail
    if place_hit and not place_hit[0]:
        prefix = (place_hit[1] or "地名没找到") + "。"
    else:
        prefix = ""
    if not key:
        return {"ok": False, "reason": prefix + "路名在路网里对不上，只有桩号没法定位"}
    return {"ok": False, "reason": prefix + "这条路没有两根已经核对过的桩号，不能沿路推算"}


def evidence_block(row):
    bits = ["证据：" + official_phrase(row["device"], row["source"])]
    if row.get("speed"):
        bits.append("限速：" + row["speed"])
    if row.get("device"):
        bits.append("设备：" + row["device"])
    if row.get("action"):
        bits.append("抓拍：" + row["action"])
    if row.get("direction"):
        bits.append("方向：" + row["direction"])
    if row.get("since"):
        bits.append("启用：" + row["since"])
    if row.get("note"):
        bits.append("备注：" + row["note"])
    return "。".join(bits) + "。"


def notes_for(row, placed, role):
    lines = [evidence_block(row).rstrip("。")]
    lines.append("定位：" + placed["how"] + "，估计误差约{:.0f}米".format(placed.get("error_m") or 0))
    lines.append("GCJ-02：" + gcj_text(placed["lat"], placed["lon"]))
    lines.append("原始清单：{} 第{}行".format(row["sheet"], row["row"]))
    if role:
        lines.append(role)
    return trim("\n".join(lines), 500)


def point_name(row, stake, role):
    parts = [row["road_code"], row["road_name"], stake_label(stake) if stake is not None else "", row.get("place") or "", row["direction"], role]
    name = " ".join(part for part in parts if part)
    name = re.sub(r"\s+", " ", name).strip()
    return trim(name, 80)


def single_point(row, placed, stake):
    role = "固定测速" if "固定" in row["device"] or "测速" in row["device"] else ""
    return {
        "id": stable_id(row["sheet"], row["row"], "point"),
        "name": point_name(row, stake, role),
        "type": "camera",
        "road_code": row["road_code"],
        "road_name": row["road_name"],
        "notes": notes_for(row, placed, ""),
        "latitude": round(placed["lat"], 6),
        "longitude": round(placed["lon"], 6),
        "violation": trim(row.get("action") or "", 200),
        "stake": stake_label(stake) if stake is not None else "",
        "direction": trim(row.get("direction") or "", 40),
        "province": row["province"],
        "city": row.get("county") or "",
    }


def interval_points(row, ends):
    a, b = ends
    stake_a = row["stakes"][0]
    stake_b = row["stakes"][1]
    chord = haversine(a["lon"], a["lat"], b["lon"], b["lat"])
    span = abs(stake_b - stake_a) * 1000.0
    straight = span > 0 and chord / span >= 0.85 and span <= 20000
    start = single_point(row, a, stake_a)
    start["id"] = stable_id(row["sheet"], row["row"], "start")
    start["name"] = point_name(row, stake_a, "区间测速起点")
    start["stake"] = trim(range_label(stake_a, stake_b), 32)
    end = single_point(row, b, stake_b)
    end["id"] = stable_id(row["sheet"], row["row"], "end")
    end["name"] = point_name(row, stake_b, "区间测速终点")
    end["stake"] = stake_label(stake_b)
    if straight:
        start["end_latitude"] = end["latitude"]
        start["end_longitude"] = end["longitude"]
        start["notes"] = trim(start["notes"] + "\n区间另一端已写入终点坐标。", 500)
    else:
        start["notes"] = trim(start["notes"] + "\n路段较长或弯曲，只标起终点，不连成直线，避免把线外的路也提醒进去。", 500)
        end["notes"] = trim(end["notes"] + "\n路段较长或弯曲，只标起终点。", 500)
    return [start, end]


def review_item(row, reason, guess, partial=False):
    item = {
        "sheet": row["sheet"],
        "row": row["row"],
        "province": row.get("province") or "",
        "county": row.get("county") or "",
        "road_code": row.get("road_code") or "",
        "road_name": row.get("road_name") or "",
        "location": row.get("raw") or row.get("text") or "",
        "device": row.get("device") or "",
        "direction": row.get("direction") or "",
        "speed": row.get("speed") or "",
        "source": row.get("source") or "",
        "reason": reason,
        "partial": partial,
        "guess_lat": "",
        "guess_lon": "",
        "guess_note": "",
    }
    if guess:
        item["guess_lat"] = round(guess.get("lat") or guess.get("latitude") or 0, 6)
        item["guess_lon"] = round(guess.get("lon") or guess.get("longitude") or 0, 6)
        item["guess_note"] = guess.get("how") or "未写入地图的大致位置"
    return item


def write_outputs(path, review_path, points, updates, review, counts):
    doc = {
        "version": 1,
        "coord": "wgs84",
        "points": points,
        "updates": updates,
    }
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as handle:
        json.dump(doc, handle, ensure_ascii=False, indent=2)
        handle.write("\n")
    csv_path = os.path.splitext(review_path)[0] + ".csv"
    os.makedirs(os.path.dirname(review_path), exist_ok=True)
    fields = [
        "sheet", "row", "province", "county", "road_code", "road_name", "location",
        "device", "direction", "speed", "source", "reason", "guess_lat", "guess_lon", "guess_note",
    ]
    with open(csv_path, "w", encoding="utf-8", newline="") as handle:
        writer = csv.DictWriter(handle, fieldnames=fields, extrasaction="ignore")
        writer.writeheader()
        for item in review:
            writer.writerow(item)
    lines = [
        "# 云南 / 海南抓拍点：未能放心标上地图的行",
        "",
        "这些行没有写入 `2026-11-yunnan-hainan.json` 的地图点。大致坐标如果有，只是对照，不能当正式点用。",
        "",
        "罚单对照表没有导入。现有点位格式只有每条点自己的罚款和记分，没有违法代码对照表。",
        "",
        "正式点在 `2026-11-yunnan-hainan.json`。在服务器上对现有库执行：",
        "`python3 import_points.py \"$DATA_DIR/motocam.db\" 2026-11-yunnan-hainan.json`。",
        "重复执行不会再插入同一条。客户端下次拉 `GET /v1/points` 就能看到，不用发新版 App。",
        "",
        "| 表 | 行 | 地点 | 原因 | 大致坐标（未采用） |",
        "| --- | --- | --- | --- | --- |",
    ]
    for item in review:
        coord = ""
        if item["guess_lat"] != "" and item["guess_lon"] != "":
            coord = "{}, {} {}".format(item["guess_lat"], item["guess_lon"], item["guess_note"])
        place = (item["location"] or "").replace("|", "/")
        reason = (item["reason"] or "").replace("|", "/")
        lines.append("| {} | {} | {} | {} | {} |".format(item["sheet"], item["row"], place, reason, coord or "—"))
    lines.append("")
    lines.append(
        "计数：新增点 {}，更新已有点 {}，因重复未再标记的表行 {}，未能定位的表行 {}，跳过的说明行 {}。".format(
            counts["added"], counts["updated"], counts["skipped_duplicate"], counts["unresolved_rows"], counts["note_rows"]
        )
    )
    lines.append("")
    with open(review_path, "w", encoding="utf-8") as handle:
        handle.write("\n".join(lines))
    return csv_path


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--yunnan", required=True)
    parser.add_argument("--hainan", required=True)
    parser.add_argument("--existing", required=True)
    parser.add_argument("--osm", action="append", default=[])
    parser.add_argument("--out", required=True)
    parser.add_argument("--review", required=True)
    parser.add_argument("--cache", default="/tmp/nominatim_ynhn.json")
    parser.add_argument("--sleep", type=float, default=1.1)
    args = parser.parse_args()
    cache = {}
    if os.path.exists(args.cache):
        with open(args.cache, encoding="utf-8") as handle:
            cache = json.load(handle)
    net, _milestones = load_osm(args.osm)
    rows = read_rows(args.yunnan, args.hainan)
    print("parsed", len(rows))
    for row in rows:
        if row["kind"] != "camera":
            print("NOTE", row["sheet"], row["row"], (row.get("text") or "")[:40])
            continue
        print(
            "ROW",
            row["sheet"],
            row["row"],
            row["road_code"],
            row["road_name"],
            ["{:.3f}".format(s) for s in row["stakes"]],
            "vague" if row["vague"] else "",
            "place=",
            row.get("place"),
            "dir=",
            row.get("direction"),
            "zone=",
            row.get("zone"),
        )
    existing = load_existing(args.existing)
    points, updates, review, counts = build(
        rows, net, existing, cache, args.sleep, os.environ.get("AMAP_KEY", "").strip()
    )
    with open(args.cache, "w", encoding="utf-8") as handle:
        json.dump(cache, handle, ensure_ascii=False)
    csv_path = write_outputs(args.out, args.review, points, updates, review, counts)
    print("COUNTS", json.dumps(counts, ensure_ascii=False))
    print("points", len(points), "updates", len(updates), "review", len(review))
    print("wrote", args.out, args.review, csv_path)
    for point in points:
        print("ADD", point["road_code"], point["name"], point["latitude"], point["longitude"])
    for update in updates:
        print("UPD", update["id"], update.get("direction"), (update.get("append_notes") or "")[:60])
    for item in review:
        print("REV", item["sheet"], item["row"], item["reason"])


if __name__ == "__main__":
    main()
