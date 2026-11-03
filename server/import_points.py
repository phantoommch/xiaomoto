#!/usr/bin/env python3
"""Load points into the community SQLite database.

Every imported report uses the fixed device id below. The server backfill
treats that device as an import, and each point is stored with source
"import" so clients do not show 「用户上传」.

The JSON file is either a list of points, or an object with "points" and an
optional "updates" list. Re-running the same file does not insert a second
copy: an existing id is left in place, and a new row whose road code and
stake already match a point within 300 m is skipped. "updates" only fills
empty fields, or appends official-source notes, on an id that is already
in the database.
"""

import json
import math
import sqlite3
import sys
import uuid
from datetime import datetime, timezone

# Fixed device id. The Go server constant ImportDeviceID must stay identical.
IMPORT_DEVICE_ID = "f4e1d000-0000-4000-8000-000000000001"
POINT_SOURCE = "import"
STAKE_MATCH_M = 300.0
STAKE_MATCH_KM = 0.30


def load_document(path):
    with open(path, encoding="utf-8") as handle:
        body = json.load(handle)
    if isinstance(body, list):
        return body, []
    if not isinstance(body, dict):
        raise SystemExit("JSON must be a list of points, or an object with points")
    points = body.get("points", [])
    updates = body.get("updates", [])
    if not isinstance(points, list) or not isinstance(updates, list):
        raise SystemExit("points and updates must be lists")
    return points, updates


def trim_runes(text, limit):
    chars = list(text)
    if len(chars) <= limit:
        return text
    return "".join(chars[:limit])


def columns_of(conn, table):
    return {row[1] for row in conn.execute("PRAGMA table_info(" + table + ")")}


def stake_kilometres(stake):
    """First Kx+y in a label, in kilometres. Same reading as store.stakeKilometres."""
    text = str(stake or "").strip().upper().replace("－", "-").replace("–", "-").replace("—", "-")
    if not text:
        return None
    # A range keeps the start, which is what a second report is matched on.
    head = text.split("-", 1)[0].strip()
    if head.startswith("K"):
        head = head[1:]
    plus = head.find("+")
    metres = 0.0
    km_part = head
    if plus >= 0:
        km_part = head[:plus].strip()
        try:
            metres = int(head[plus + 1:].strip() or "0") / 1000.0
        except ValueError:
            return None
    try:
        return float(km_part) + metres
    except ValueError:
        return None


def haversine_m(lat1, lon1, lat2, lon2):
    r = 6371000.0
    p1 = math.radians(lat1)
    p2 = math.radians(lat2)
    dlat = math.radians(lat2 - lat1)
    dlon = math.radians(lon2 - lon1)
    h = math.sin(dlat / 2) ** 2 + math.cos(p1) * math.cos(p2) * math.sin(dlon / 2) ** 2
    return 2 * r * math.asin(min(1.0, math.sqrt(h)))


def find_stake_match(conn, road, stake, lat, lon):
    km = stake_kilometres(stake)
    road = str(road or "").strip().upper()
    if km is None or not road:
        return None
    rows = conn.execute(
        "SELECT id, latitude, longitude, stake FROM points WHERE road_code=?",
        (road,),
    )
    best = None
    best_dist = STAKE_MATCH_M + 1
    for point_id, plat, plon, other in rows:
        other_km = stake_kilometres(other)
        if other_km is None or abs(other_km - km) > STAKE_MATCH_KM:
            continue
        dist = haversine_m(lat, lon, plat, plon)
        if dist <= STAKE_MATCH_M and dist < best_dist:
            best = point_id
            best_dist = dist
    return best


def insert_points(db_path, points, updates):
    conn = sqlite3.connect(db_path)
    conn.row_factory = sqlite3.Row
    try:
        point_cols = columns_of(conn, "points")
        report_cols = columns_of(conn, "reports")
        if "source" not in point_cols:
            raise SystemExit("points.source is missing; start the server once before importing")
        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        written = 0
        skipped = 0
        updated = 0
        for raw in points:
            outcome = insert_one(conn, point_cols, report_cols, raw, now)
            if outcome == "inserted":
                written += 1
            else:
                skipped += 1
        for raw in updates:
            if apply_update(conn, point_cols, raw, now):
                updated += 1
            else:
                skipped += 1
        conn.commit()
        return written, updated, skipped
    finally:
        conn.close()


def insert_one(conn, point_cols, report_cols, raw, now):
    name = str(raw.get("name") or "").strip()
    typ = str(raw.get("type") or "other").strip()
    lat = raw.get("latitude")
    lon = raw.get("longitude")
    if not name or lat is None or lon is None:
        raise SystemExit("each point needs name, latitude, and longitude")
    point_id = str(raw.get("id") or uuid.uuid4())
    existing = conn.execute("SELECT id FROM points WHERE id=?", (point_id,)).fetchone()
    if existing:
        return "skip-id"
    road = str(raw.get("road_code") or "").strip().upper()
    stake = str(raw.get("stake") or "")
    match = find_stake_match(conn, road, stake, float(lat), float(lon))
    if match:
        return "skip-stake"
    fields = {
        "id": point_id,
        "name": name,
        "type": typ,
        "road_code": road,
        "road_name": str(raw.get("road_name") or ""),
        "notes": trim_runes(str(raw.get("notes") or ""), 500),
        "latitude": float(lat),
        "longitude": float(lon),
        "fine_yuan": raw.get("fine_yuan"),
        "penalty_points": raw.get("penalty_points"),
        "violation": trim_runes(str(raw.get("violation") or ""), 200),
        "source_count": 1,
        "created_at": now,
        "updated_at": now,
        "source": POINT_SOURCE,
        "stake": trim_runes(stake.strip(), 32),
        "direction": trim_runes(str(raw.get("direction") or "").strip(), 40),
        "end_latitude": raw.get("end_latitude"),
        "end_longitude": raw.get("end_longitude"),
    }
    write_row(conn, "points", point_cols, fields)
    report = {
        "id": str(uuid.uuid4()),
        "device_id": IMPORT_DEVICE_ID,
        "name": name,
        "type": typ,
        "road_code": road,
        "road_name": fields["road_name"],
        "notes": fields["notes"],
        "latitude": float(lat),
        "longitude": float(lon),
        "fine_yuan": raw.get("fine_yuan"),
        "penalty_points": raw.get("penalty_points"),
        "violation": fields["violation"],
        "status": "merged",
        "public_point_id": point_id,
        "created_at": now,
        "located": 1,
        "stake": fields["stake"],
        "direction": fields["direction"],
        "end_latitude": fields["end_latitude"],
        "end_longitude": fields["end_longitude"],
    }
    write_row(conn, "reports", report_cols, report)
    return "inserted"


def write_row(conn, table, present, fields):
    cols = [name for name in fields if name in present]
    sql = "INSERT INTO " + table + " (" + ", ".join(cols) + ") VALUES (" + ", ".join("?" for _ in cols) + ")"
    conn.execute(sql, [fields[name] for name in cols])


def apply_update(conn, point_cols, raw, now):
    point_id = str(raw.get("id") or "").strip()
    if not point_id:
        raise SystemExit("each update needs id")
    row = conn.execute("SELECT * FROM points WHERE id=?", (point_id,)).fetchone()
    if row is None:
        return False
    changes = {}
    direction = str(raw.get("direction") or "").strip()
    if direction and not str(row["direction"] or "").strip() and "direction" in point_cols:
        changes["direction"] = trim_runes(direction, 40)
    stake = str(raw.get("stake") or "").strip()
    if stake and not str(row["stake"] or "").strip() and "stake" in point_cols:
        changes["stake"] = trim_runes(stake, 32)
    violation = str(raw.get("violation") or "").strip()
    if violation and not str(row["violation"] or "").strip():
        changes["violation"] = trim_runes(violation, 200)
    if row["end_latitude"] is None and raw.get("end_latitude") is not None and "end_latitude" in point_cols:
        changes["end_latitude"] = float(raw["end_latitude"])
        changes["end_longitude"] = float(raw["end_longitude"])
    extra = str(raw.get("append_notes") or "").strip()
    old_notes = str(row["notes"] or "")
    if extra and extra not in old_notes:
        joined = (old_notes + "\n" + extra).strip() if old_notes else extra
        changes["notes"] = trim_runes(joined, 500)
    if not changes:
        return False
    changes["updated_at"] = now
    sets = ", ".join(name + "=?" for name in changes)
    conn.execute(
        "UPDATE points SET " + sets + " WHERE id=?",
        list(changes.values()) + [point_id],
    )
    return True


def main(argv):
    if len(argv) != 3:
        raise SystemExit("usage: import_points.py <motocam.db> <points.json>")
    points, updates = load_document(argv[2])
    written, updated, skipped = insert_points(argv[1], points, updates)
    print(f"imported {written} updated {updated} skipped {skipped}")


if __name__ == "__main__":
    main(sys.argv)
