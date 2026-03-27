import json
import os
import re
from pathlib import Path
from typing import Any, Dict, List, Tuple

from rapidfuzz import fuzz
from slack_bolt import App
from slack_bolt.adapter.socket_mode import SocketModeHandler

DATA_PATH = Path("data/specs.json")


def normalize(text: str) -> str:
    text = text.lower().strip()
    text = re.sub(r"[^\w\s\-\/\.]", " ", text)
    text = re.sub(r"\s+", " ", text)
    return text.strip()


def load_specs() -> List[Dict[str, Any]]:
    with open(DATA_PATH, "r", encoding="utf-8") as f:
        return json.load(f)


SPECS = load_specs()


def build_index(specs: List[Dict[str, Any]]) -> List[Dict[str, str]]:
    rows = []
    for item in specs:
        spec = item["spec"]
        pod = item["pod"]

        rows.append({
            "alias": normalize(spec),
            "spec": spec,
            "pod": pod,
            "source": spec,
        })

        for alias in item.get("aliases", []):
            rows.append({
                "alias": normalize(alias),
                "spec": spec,
                "pod": pod,
                "source": alias,
            })
    return rows


INDEX = build_index(SPECS)


def get_notes(spec_name: str) -> List[str]:
    for item in SPECS:
        if item["spec"] == spec_name:
            return item.get("notes", [])
    return []


def exact_match(query: str) -> Dict[str, str] | None:
    for row in INDEX:
        if row["alias"] == query:
            return row
    return None


def partial_matches(query: str) -> List[Dict[str, str]]:
    matches = []
    for row in INDEX:
        if query in row["alias"] or row["alias"] in query:
            matches.append(row)

    dedup = {}
    for row in matches:
        dedup[row["spec"]] = row
    return list(dedup.values())


def fuzzy_matches(query: str, limit: int = 3) -> List[Tuple[int, Dict[str, str]]]:
    scored = []
    for row in INDEX:
        score = fuzz.token_sort_ratio(query, row["alias"])
        if score >= 60:
            scored.append((score, row))

    scored.sort(key=lambda x: x[0], reverse=True)

    seen = set()
    result = []
    for score, row in scored:
        if row["spec"] in seen:
            continue
        seen.add(row["spec"])
        result.append((score, row))
        if len(result) >= limit:
            break
    return result


def resolve(raw_query: str) -> Dict[str, Any]:
    query = normalize(raw_query)
    if not query:
        return {"status": "empty"}

    hit = exact_match(query)
    if hit:
        return {
            "status": "matched",
            "spec": hit["spec"],
            "pod": hit["pod"],
            "reason": f'Exact match on "{hit["source"]}"',
            "notes": get_notes(hit["spec"]),
        }

    partial = partial_matches(query)
    if len(partial) == 1:
        hit = partial[0]
        return {
            "status": "matched",
            "spec": hit["spec"],
            "pod": hit["pod"],
            "reason": f'Partial match on "{hit["source"]}"',
            "notes": get_notes(hit["spec"]),
        }

    if len(partial) > 1:
        return {
            "status": "ambiguous",
            "candidates": [
                {
                    "spec": row["spec"],
                    "pod": row["pod"],
                    "reason": f'Partial match on "{row["source"]}"',
                }
                for row in partial[:3]
            ],
        }

    fuzzy = fuzzy_matches(query)
    if len(fuzzy) == 1:
        score, hit = fuzzy[0]
        return {
            "status": "matched",
            "spec": hit["spec"],
            "pod": hit["pod"],
            "reason": f'Fuzzy match on "{hit["source"]}" (score={score})',
            "notes": get_notes(hit["spec"]),
        }

    if len(fuzzy) > 1:
        return {
            "status": "ambiguous",
            "candidates": [
                {
                    "spec": row["spec"],
                    "pod": row["pod"],
                    "reason": f'Fuzzy match on "{row["source"]}" (score={score})',
                }
                for score, row in fuzzy
            ],
        }

    return {"status": "not_found"}


app = App(token=os.environ["SLACK_BOT_TOKEN"])


@app.command("/specwho")
def handle_specwho(ack, respond, command):
    ack()

    raw_query = command.get("text", "").strip()
    result = resolve(raw_query)

    if result["status"] == "empty":
        respond(
            response_type="ephemeral",
            text="Please provide a product or feature name. Example: `/specwho agent integrations`",
        )
        return

    if result["status"] == "matched":
        text = (
            f"*Spec:* {result['spec']}\n"
            f"*Pod:* {result['pod']}\n"
            f"*Why:* {result['reason']}"
        )
        notes = result.get("notes", [])[:3]
        if notes:
            text += "\n*Notes:*\n" + "\n".join(f"• {n}" for n in notes)

        respond(response_type="ephemeral", text=text)
        return

    if result["status"] == "ambiguous":
        lines = []
        for i, c in enumerate(result["candidates"], start=1):
            lines.append(f"{i}. *{c['spec']}* -> {c['pod']}\n   {c['reason']}")
        respond(
            response_type="ephemeral",
            text="I found multiple likely matches:\n" + "\n".join(lines),
        )
        return

    respond(
        response_type="ephemeral",
        text=f'No match found for "{raw_query}". Try a more specific term.',
    )


if __name__ == "__main__":
    handler = SocketModeHandler(app, os.environ["SLACK_APP_TOKEN"])
    handler.start()
