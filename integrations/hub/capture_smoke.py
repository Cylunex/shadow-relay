"""Capture Hub fixtures on explicit invocation inside the Hub Python environment.

Usage: PYTHONPATH=/path/to/hub/backend python capture_smoke.py PLUGIN_DIR
       --keyword 'REPLACE_BOOK_TITLE' --expected-count 123
This is an operator tool; Relay never runs it automatically.
"""
import argparse
import asyncio
import hashlib
import json
from pathlib import Path


async def capture(args):
    from app.source_plugins.context import PluginContext
    from app.source_plugins.fetcher import Fetcher
    from app.source_plugins.loader import PluginLoader

    plugin_dir = Path(args.plugin_dir).resolve()
    if not plugin_dir.name.startswith("relay_"):
        raise ValueError("capture is restricted to generated Relay plugins")
    plugin = PluginLoader(plugins_dir=plugin_dir).load_all().get(plugin_dir.name)
    if plugin is None:
        raise ValueError("Hub could not load this plugin")
    responses, stages = {}, {}
    current = "search"

    class Recorder(Fetcher):
        last_fetch = 0.0

        async def fetch_text(self, url, **kwargs):
            loop = asyncio.get_running_loop()
            interval = max(1.2, float(plugin.metadata.rate_limit.get("minIntervalMs", 1200)) / 1000)
            await asyncio.sleep(max(0.0, self.last_fetch + interval - loop.time()))
            self.last_fetch = loop.time()
            value = await super().fetch_text(url, **kwargs)
            if len(value.encode("utf-8")) > 4 * 1024 * 1024 or len(responses) >= 200:
                raise ValueError("fixture capture exceeds size limit")
            responses[url] = value
            stages.setdefault(current, url)
            return value

    ctx = PluginContext(fetcher=Recorder(), plugin_id=plugin.metadata.id, cookie_allowed=False)
    source = plugin.source
    results = await asyncio.wait_for(source.search(ctx, args.keyword, 1), 90)
    if not results:
        raise ValueError("search returned no results")
    current = "detail"
    detail = await asyncio.wait_for(source.detail(ctx, results[0]["bookUrl"]), 60)
    current = "toc"
    toc = await asyncio.wait_for(source.toc(ctx, detail["tocUrl"]), 300)
    if not toc or len({c["chapterUrl"] for c in toc}) != len(toc):
        raise ValueError("directory is empty or contains duplicate chapter URLs")
    if [c["index"] for c in toc] != list(range(1, len(toc) + 1)):
        raise ValueError("directory indexes are not sequential")
    if args.expected_count is not None and len(toc) != args.expected_count:
        raise ValueError("directory count does not match the independently confirmed expected count")
    latest = detail.get("lastChapter", "").strip()
    if latest and "".join(latest.split()) != "".join(toc[-1]["title"].split()):
        raise ValueError("directory tail does not match the latest chapter in detail")
    current = "chapter"
    chapter = await asyncio.wait_for(source.chapter(ctx, toc[0]["chapterUrl"]), 120)
    if len(chapter.get("content", "")) < 200:
        raise ValueError("sample chapter is too short")
    complete = args.expected_count is not None
    # A tail match alone cannot prove that no middle chapters were omitted.
    target = plugin_dir / "smoke"
    (target / "fixtures").mkdir(parents=True, exist_ok=True)
    filenames = {}
    for url, body in responses.items():
        filename = hashlib.sha256(url.encode()).hexdigest()[:20] + ".html"
        (target / "fixtures" / filename).write_text(body, encoding="utf-8")
        filenames[url] = filename
    spec = {
        "keyword": args.keyword,
        "fixtures": {stage: {"url": url, "file": filenames[url]} for stage, url in stages.items()},
        "extraFixtures": [{"url": url, "file": name} for url, name in filenames.items() if url not in stages.values()],
        "expect": {
            "search": {"minResults": 1, "firstName": results[0]["name"]},
            "detail": {"name": detail["name"], "hasTocUrl": True},
            "toc": {"complete": complete, "expectedCount": len(toc), "minChapters": 1,
                    "firstTitleContains": toc[0]["title"], "lastTitleContains": toc[-1]["title"],
                    "requireUniqueChapterUrls": True, "requireSequentialIndexes": True},
            "chapter": {"minContentLength": 200},
        },
        "relayDraft": not complete,
    }
    (target / "smoke.yaml").write_text(json.dumps(spec, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps({"capturedResponses": len(responses), "chapters": len(toc),
                      "complete": complete, "fixtureTestsRun": False}))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("plugin_dir")
    parser.add_argument("--keyword", required=True)
    parser.add_argument("--expected-count", type=int)
    asyncio.run(capture(parser.parse_args()))
