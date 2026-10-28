#!/usr/bin/env python3
"""Download official CGU daily expenditure archives into ignored .data/.

Usage: python3 research/brazil-procurement-recon/acquire.py 20261023 20261022
"""
import argparse
import hashlib
import json
from datetime import datetime, timezone
from pathlib import Path
import requests

ROOT = Path(__file__).resolve().parents[2]
RAW = ROOT / ".data" / "brazil-procurement-recon" / "raw"
MANIFEST = RAW / "manifest.jsonl"
BASE = "https://portaldatransparencia.gov.br/download-de-dados/despesas/"


def download(day: str) -> None:
    if len(day) != 8 or not day.isdigit():
        raise ValueError(f"Expected YYYYMMDD: {day}")
    RAW.mkdir(parents=True, exist_ok=True)
    source_url = BASE + day
    response = requests.get(source_url, stream=True, timeout=(30, 120))
    response.raise_for_status()
    if "zip" not in response.headers.get("content-type", "").lower():
        raise RuntimeError(f"Not a ZIP response: {source_url}: {response.headers}")
    path = RAW / f"{day}_Despesas.zip"
    temp = path.with_suffix(".zip.part")
    digest = hashlib.sha256()
    size = 0
    with temp.open("wb") as output:
        for chunk in response.iter_content(1024 * 1024):
            if chunk:
                output.write(chunk)
                digest.update(chunk)
                size += len(chunk)
    temp.replace(path)
    record = {
        "day": day,
        "source_url": source_url,
        "final_url": response.url,
        "retrieved_utc": datetime.now(timezone.utc).isoformat(),
        "http_status": response.status_code,
        "content_type": response.headers.get("content-type"),
        "content_length_header": response.headers.get("content-length"),
        "bytes": size,
        "sha256": digest.hexdigest(),
        "path": str(path.relative_to(ROOT)),
    }
    with MANIFEST.open("a", encoding="utf-8") as output:
        output.write(json.dumps(record, ensure_ascii=False) + "\n")
    print(json.dumps(record, ensure_ascii=False))


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("days", nargs="+", help="YYYYMMDD dates listed on official page")
    args = parser.parse_args()
    for day in args.days:
        download(day)
