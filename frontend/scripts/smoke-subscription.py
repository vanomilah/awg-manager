#!/usr/bin/env python3
"""Smoke test for /subscriptions section.

Run with mock-proxy already up:
    cd frontend && npm run dev:mock:proxy &
    uv run --with playwright python3 scripts/smoke-subscription.py
"""

from __future__ import annotations
import os
import sys
from playwright.sync_api import sync_playwright

BASE = os.environ.get("BASE", "http://127.0.0.1:5173")
HEADLESS = os.environ.get("HEADLESS", "1") != "0"


def main() -> int:
    failed = False
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=HEADLESS)
        page = browser.new_page(viewport={"width": 1600, "height": 900})
        try:
            print("[smoke-sub] navigate /subscriptions")
            page.goto(f"{BASE}/subscriptions")
            page.wait_for_load_state("networkidle")

            print("[smoke-sub] click + Добавить")
            page.locator("a, button").filter(has_text="Добавить").first.click()
            page.wait_for_url("**/subscriptions/new", timeout=5000)

            print("[smoke-sub] fill form")
            page.locator('input[type="text"]').first.fill("Test Sub")
            page.locator('input[type="url"]').first.fill("https://example.com/sub")
            page.locator('button[type="submit"]').click()

            print("[smoke-sub] await detail page")
            page.wait_for_url("**/subscriptions/sub-*", timeout=10000)

            print("[smoke-sub] verify selector tag visible")
            page.wait_for_selector('text=/sub-/', timeout=5000)

            print("[smoke-sub] OK")
        except Exception as e:
            failed = True
            print(f"[smoke-sub] FAIL: {e}", file=sys.stderr)
            try:
                page.screenshot(path="/tmp/smoke-subscription-fail.png", full_page=True)
            except Exception:
                pass
        browser.close()
    return 1 if failed else 0


if __name__ == "__main__":
    raise SystemExit(main())
