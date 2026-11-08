#!/usr/bin/env python3
"""Lint a .cook recipe against the household conventions. Usage: validate.py Name.cook [...]"""
import re
import sys

ORDER = ["title", "aliases", "source", "image source", "course", "season", "tags",
         "difficulty", "prep time", "cook time", "servings", "created"]
REQUIRED = {"title", "course", "season", "tags", "difficulty", "servings", "created"}
SEASONS = {"Spring", "Summer", "Fall", "Winter", "Year-round"}
COURSES = {"Main", "Dessert", "Drinks", "Side", "Breakfast", "Snack"}
DIFFS = {"Easy", "Medium", "Hard"}
ING = re.compile(r"@[?&-]*[^@#~{}\n]+?\{[^}\n]*\}(?:\([^)\n]*\))?")


def check(path):
    errs = []
    text = open(path, encoding="utf-8").read()
    m = re.match(r"---\n(.*?)\n---\n(.*)", text, re.S)
    if not m:
        return ["no frontmatter block"]
    fm, body = m.groups()
    keys = re.findall(r"^([A-Za-z][^:\n]*):", fm, re.M)
    vals = dict(re.findall(r"^([A-Za-z][^:\n]*):[ ]*(.*)$", fm, re.M))
    for k in REQUIRED - set(keys):
        errs.append(f"missing key: {k}")
    for k in keys:
        if k not in ORDER:
            errs.append(f"unexpected key: {k}")
    known = [k for k in keys if k in ORDER]
    if known != sorted(known, key=ORDER.index):
        errs.append("keys out of order")
    if vals.get("season") not in SEASONS:
        errs.append(f"bad season: {vals.get('season')}")
    if vals.get("course") not in COURSES:
        errs.append(f"bad course: {vals.get('course')}")
    if vals.get("difficulty") not in DIFFS:
        errs.append(f"bad difficulty: {vals.get('difficulty')}")
    if not re.fullmatch(r"[1-9]\d*", vals.get("servings", "")):
        errs.append(f"servings must be a plain integer: {vals.get('servings')!r}")
    if not re.fullmatch(r"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d", vals.get("created", "")):
        errs.append("created must be YYYY-MM-DDTHH:MM:SS")
    tags = re.search(r"^tags:\n((?:\s*- .*\n?)+)", fm + "\n", re.M)
    if not tags:
        errs.append("tags must be a '- tag' list")
    else:
        for t in re.findall(r"^\s*- (.*)$", tags.group(1), re.M):
            if not re.fullmatch(r"[a-z0-9]+(-[a-z0-9]+)*", t):
                errs.append(f"tag not lowercase-kebab: {t}")
    if not ING.search(body):
        errs.append("body has no @ingredient{} markup")
    if re.search(r"^>>", body, re.M):
        errs.append("body has >> metadata lines")
    rest = ING.sub("", body)
    if re.search(r"@[A-Za-z]", rest):
        errs.append("stray @ without braces")
    if re.search(r"(?<![\w&])#(?![^{\n]*\{)\w", rest):
        errs.append("stray # without braces")
    for note in re.findall(r"\}\(([^)\n]*)\)", body):
        if re.search(r"\boptional\b", note, re.I):
            errs.append(f"use @? instead of 'optional' in a note: ({note})")
    for q in re.findall(r"[@~][^{\n]*\{([^}\n]*)\}", body):
        if q and "%" in q:
            amt = q.split("%")[0].strip().lstrip("=")
            num = r"(?:\d+(?:\.\d+)?(?: \d+/\d+)?|\d+/\d+)"
            if not re.fullmatch(num + "(?:-" + num + ")?", amt):
                errs.append(f"odd quantity: {q}")
    return errs


bad = 0
for p in [a for a in sys.argv[1:] if not a.startswith("--")]:
    e = check(p)
    if e:
        bad += 1
        print(f"{p}:")
        for x in e:
            print("  -", x)
    else:
        print(f"{p}: ok")
sys.exit(1 if bad else 0)
