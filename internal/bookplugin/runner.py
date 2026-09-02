"""Shadow Relay declarative adapter for the LegadoHub 1.0 plugin contract.

Only the host performs HTTP, authentication, caching and concurrency control.
This file is shipped by Relay; imported rules are JSON data, never Python or JS.
"""
import json
import re
from pathlib import Path
from urllib.parse import parse_qs, quote_plus, urljoin, urlsplit, urlunsplit

_CONFIG = json.loads(Path(__file__).with_name("recipe.json").read_text("utf-8"))
_RULE = _CONFIG["recipe"]


class Source:
    id = _CONFIG["id"]
    name = _RULE["name"]
    contract_version = "1.0"
    last_modified = _CONFIG["updated"]

    def _url(self, base, value):
        if not value:
            return ""
        target = urlsplit(urljoin(base, str(value)))
        if (target.scheme not in ("https", "http") or target.username or
                target.password or target.hostname not in _RULE["domains"]):
            raise ValueError("PARSE_ERROR: link leaves the declared source domains")
        return urlunsplit((target.scheme, target.netloc, target.path, target.query, ""))

    async def _fetch(self, ctx, target, stage=None, keyword="", page=1):
        target = self._url(_RULE["baseUrl"], target)
        kwargs = {"timeout": 20}
        if stage and stage.get("method") == "POST":
            kwargs["method"] = "POST"
            kwargs["data"] = {k: v.replace("{keyword}", keyword).replace("{page}", str(page))
                              for k, v in stage.get("form", {}).items()}
        body = await ctx.access.http.fetch_text(target, **kwargs)
        if len(body.encode("utf-8")) > 4 * 1024 * 1024:
            raise ValueError("PARSE_ERROR: response exceeds 4 MiB")
        return body

    def _nodes(self, ctx, doc, selector):
        if not selector:
            return []
        if selector.get("path"):
            if isinstance(doc, str) and doc.lstrip().startswith(("{", "[", '"')):
                doc = json.loads(doc)
            nodes = [doc]
            for field, index in re.findall(r"\.([A-Za-z_][\w-]*)|\[(\d+|\*)\]", selector["path"][1:]):
                result = []
                for node in nodes:
                    if field and isinstance(node, dict) and field in node:
                        result.append(node[field])
                    elif index == "*" and isinstance(node, list):
                        result.extend(node)
                    elif index and index != "*" and isinstance(node, list) and int(index) < len(node):
                        result.append(node[int(index)])
                nodes = result
            return nodes
        css = selector.get("css", "$self")
        return [doc] if css == "$self" else ctx.select(doc, css)

    def _list(self, ctx, doc, selector):
        nodes = self._nodes(ctx, doc, selector)
        result = []
        for node in nodes:
            result.extend(node if isinstance(node, list) else [node])
        if len(result) > 20000:
            raise ValueError("PARSE_ERROR: selector returned too many items")
        return result

    def _value(self, ctx, doc, selector):
        nodes = self._nodes(ctx, doc, selector)
        if not nodes:
            return ""
        if selector.get("path"):
            return "\n\n".join(str(n) for n in nodes if isinstance(n, (str, int, float)))
        if selector.get("attr"):
            node = nodes[0]
            if isinstance(node, str):
                node = ctx.select(node, "*")[0]
            return node.get(selector["attr"], "")
        if selector.get("html"):
            return "\n\n".join(ctx.clean_html(ctx.html(n)) for n in nodes)
        return "\n\n".join(ctx.text(n) for n in nodes)

    def _fields(self, ctx, doc, stage, base):
        result = {k: self._value(ctx, doc, sel) for k, sel in stage.get("fields", {}).items()}
        for key in ("bookUrl", "tocUrl", "chapterUrl"):
            if result.get(key):
                result[key] = self._url(base, result[key])
        # Covers may use another CDN; host/client fetch policy still applies.
        if result.get("coverUrl"):
            cover = urljoin(base, result["coverUrl"])
            parsed = urlsplit(cover)
            result["coverUrl"] = cover if parsed.scheme in ("https", "http") and not parsed.username else ""
        if _RULE.get("language") in ("zh-TW", "zh-Hant"):
            for key in result:
                if not key.endswith("Url"):
                    result[key] = ctx.to_simplified(result[key])
        result["sourceId"] = self.id
        return result

    async def search(self, ctx, keyword, page=1):
        stage = _RULE["search"]
        query = ctx.to_traditional(keyword) if _RULE.get("language") in ("zh-TW", "zh-Hant") else keyword
        target = stage["url"].replace("{keyword}", quote_plus(query)).replace("{page}", str(max(1, int(page))))
        body = await self._fetch(ctx, target, stage, query, page)
        result, seen = [], set()
        for node in self._list(ctx, body, stage["list"])[:100]:
            item = self._fields(ctx, node, stage, self._url(_RULE["baseUrl"], target))
            book_url = item.get("bookUrl")
            if not item.get("name") or not book_url or book_url in seen:
                continue
            seen.add(book_url)
            result.append(item)
        for item in result[:3]:
            if keyword.strip() in item["name"] and any(not item.get(k) for k in ("author", "coverUrl", "lastChapter")):
                try:
                    detail = await self.detail(ctx, item["bookUrl"])
                    for key, value in detail.items():
                        if not item.get(key):
                            item[key] = value
                    item["extra"] = {"detailEnriched": True}
                except Exception:
                    ctx.trace("detail", message="Relay search enrichment unavailable")
        return result

    async def detail(self, ctx, book_url):
        body = await self._fetch(ctx, book_url)
        result = self._fields(ctx, body, _RULE["detail"], book_url)
        result.update(bookUrl=book_url, tocUrl=result.get("tocUrl") or book_url)
        if not result.get("name"):
            raise ValueError("PARSE_EMPTY: detail has no book name")
        return result

    async def toc(self, ctx, toc_url):
        stage = _RULE["toc"]
        target, pages, seen, result = toc_url, set(), set(), []
        while target:
            if target in pages or len(pages) >= _RULE["maxPages"]:
                raise ValueError("PARSE_ERROR: TOC pagination cycle or limit; refusing an incomplete directory")
            pages.add(target)
            body = await self._fetch(ctx, target)
            for node in self._list(ctx, body, stage["list"]):
                item = self._fields(ctx, node, stage, target)
                chapter_url = item.get("chapterUrl")
                if not item.get("title") or not chapter_url or chapter_url in seen:
                    continue
                seen.add(chapter_url)
                result.append(item)
                if len(result) > 20000:
                    raise ValueError("PARSE_ERROR: too many chapters")
            target = self._url(target, self._value(ctx, body, stage.get("next", {})))
        if stage.get("reverse"):
            result.reverse()
        for index, item in enumerate(result, 1):
            item.update(index=index, isVip=False, isLocked=False)
        if not result:
            raise ValueError("PARSE_EMPTY: empty TOC")
        return result

    def _same_chapter(self, current, following):
        a, b = urlsplit(current), urlsplit(following)
        if a.netloc != b.netloc:
            return False
        if a.path != b.path:
            # Preserve the original chapter ID. Stripping numeric suffixes from
            # both URLs would incorrectly merge chapter-1 with chapter-2.
            parent, _, filename = a.path.rpartition("/")
            stem, dot, extension = filename.rpartition(".")
            if not dot:
                stem, extension = filename, ""
            prefix = parent + "/" + stem
            suffix = "." + extension if dot else ""
            match = re.fullmatch(re.escape(prefix) + r"[_-](\d+)" + re.escape(suffix), b.path)
            if not match or int(match.group(1)) < 2:
                return False
        def identity(query):
            return {k: v for k, v in parse_qs(query).items() if k not in ("page", "p", "pageNo")}
        return identity(a.query) == identity(b.query)

    async def chapter(self, ctx, chapter_url):
        stage = _RULE["chapter"]
        target, pages, texts, title = chapter_url, set(), [], ""
        while target:
            if target in pages or len(pages) >= min(_RULE["maxPages"], 20):
                raise ValueError("PARSE_ERROR: chapter pagination cycle or limit")
            pages.add(target)
            body = await self._fetch(ctx, target)
            next_url = self._url(target, self._value(ctx, body, stage.get("next", {})))
            if stage.get("removeCss") and not stage["fields"]["content"].get("path"):
                root = ctx.select(body, "html")
                if root:
                    for node in ctx.select(root[0], stage["removeCss"]):
                        parent = node.getparent()
                        if parent is not None:
                            parent.remove(node)
                    body = ctx.html(root[0])
            item = self._fields(ctx, body, stage, target)
            title = title or item.get("title", "")
            content = item.get("content", "").strip()
            if not content:
                raise ValueError("PARSE_EMPTY: chapter content is empty")
            texts.append(content)
            if sum(map(len, texts)) > 1000000:
                raise ValueError("PARSE_ERROR: chapter exceeds content limit")
            target = next_url if next_url and self._same_chapter(chapter_url, next_url) else ""
        return {"sourceId": self.id, "title": title, "chapterUrl": chapter_url,
                "content": "\n\n".join(texts), "format": "text", "authRequired": False, "isPaid": False}
