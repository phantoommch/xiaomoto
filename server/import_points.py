#!/usr/bin/env python3
"""Load points into the community SQLite database.

Every imported report uses the fixed device id below. The server backfill
treats that device as an import, and each point is stored with source
"import" so clients do not show 「用户上传」.
"""

import json
import sqlite3
import sys
import uuid
from datetime import datetime, timezone

# Fixed device id. The Go server constant ImportDeviceID must stay identical.
IMPORT_DEVICE_ID = "f4e1d000-0000-4000-8000-000000000001"
POINT_SOURCE = "import"


def load_document(path):
    with open(path, encoding="utf-8") as handle:
        body = json.load(handle)
    if isinstance(body, dict):
        body = body.get("points", [])
    if not isinstance(body, list):
        raise SystemExit("JSON must be a list of points, or an object with points")
    return body


def insert_points(db_path, points):
    conn = sqlite3.connect(db_path)
    try:
        columns = {row[1] for row in conn.execute("PRAGMA table_info(points)")}
        if "source" not in columns:
            raise SystemExit("points.source is missing; start the server once before importing")
        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        written = 0
        for raw in points:
            name = str(raw.get("name") or "").strip()
            typ = str(raw.get("type") or "other").strip()
            lat = raw.get("latitude")
            lon = raw.get("longitude")
            if not name or lat is None or lon is None:
                raise SystemExit("each point needs name, latitude, and longitude")
            point_id = str(raw.get("id") or uuid.uuid4())
            report_id = str(uuid.uuid4())
            conn.execute(
                """
INSERT INTO points (
  id, name, type, road_code, road_name, notes, latitude, longitude,
  fine_yuan, penalty_points, violation, source_count, created_at, updated_at, source
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)
""",
                (
                    point_id,
                    name,
                    typ,
                    str(raw.get("road_code") or ""),
                    str(raw.get("road_name") or ""),
                    str(raw.get("notes") or ""),
                    float(lat),
                    float(lon),
                    raw.get("fine_yuan"),
                    raw.get("penalty_points"),
                    str(raw.get("violation") or ""),
                    now,
                    now,
                    POINT_SOURCE,
                ),
            )
            conn.execute(
                """
INSERT INTO reports (
  id, device_id, name, type, road_code, road_name, notes, latitude, longitude,
  fine_yuan, penalty_points, violation, status, public_point_id, created_at, located
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'merged', ?, ?, 1)
""",
                (
                    report_id,
                    IMPORT_DEVICE_ID,
                    name,
                    typ,
                    str(raw.get("road_code") or ""),
                    str(raw.get("road_name") or ""),
                    str(raw.get("notes") or ""),
                    float(lat),
                    float(lon),
                    raw.get("fine_yuan"),
                    raw.get("penalty_points"),
                    str(raw.get("violation") or ""),
                    point_id,
                    now,
                ),
            )
            written += 1
        conn.commit()
        return written
    finally:
        conn.close()


def main(argv):
    if len(argv) != 3:
        raise SystemExit("usage: import_points.py <motocam.db> <points.json>")
    count = insert_points(argv[1], load_document(argv[2]))
    print(f"imported {count}")


if __name__ == "__main__":
    main(sys.argv)
