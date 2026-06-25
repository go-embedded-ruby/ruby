#!/usr/bin/env python3
"""categorize.py — group heavyweight parse-conformance failures by syntax construct.

Reads the TSVs emitted by sweep.sh for one repo and, for every *real front-end
gap* (rbgo rejects but MRI `ruby -c` accepts — i.e. valid Ruby rbgo can't parse),
pulls the offending source line and assigns it to a category. Categories are
ranked by the number of distinct FILES each one blocks, because a single missing
construct (e.g. the leading "::" scope-resolution operator) can account for
thousands of file rejections.

Usage:
    categorize.py <out_dir> <repo>            # e.g. /tmp/ger-heavyweight-out rails

Output: a ranked table (files-blocked, category) plus, per category, up to a few
representative offending lines reduced toward a minimal repro.
"""
import os
import re
import sys

# Each rule: (category, predicate-on-stripped-line). First match wins; order
# matters (most specific first). These describe the *valid Ruby* rbgo rejects.
def categorize(line, msg):
    s = line.strip()
    m = msg.lower()
    # --- scope resolution "::" -------------------------------------------
    if re.search(r'<\s*::[A-Z]', s):
        return 'superclass `< ::Const` (leading scope-resolution)'
    if re.search(r'class\s+\S+\s*<\s*[A-Z][\w]*::', s):
        return 'superclass `< A::B` (qualified constant)'
    if re.search(r'(^|[^\w:])::[A-Z]', s):
        return 'leading `::Const` (top-level scope resolution)'
    # --- pattern matching -------------------------------------------------
    if re.match(r'(in|case)\b', s) and ('=>' in s or ' in ' in s or '[' in s or '{' in s):
        if 'in ' in s or s.startswith('in'):
            return 'pattern matching (`case/in`, `=>`, find/array/hash patterns)'
    if ' => ' in s and re.match(r'in\b', s):
        return 'pattern matching (`case/in`, `=>`, find/array/hash patterns)'
    # --- endless / one-line method def -----------------------------------
    if re.match(r'def\s+\S+.*=\s*\S', s) and '=' in s.split('(')[0] + s:
        if re.search(r'def\s+[\w?!=]+(\([^)]*\))?\s*=\s*\S', s):
            return 'endless method def (`def foo = expr`)'
    # --- safe navigation --------------------------------------------------
    if '&.' in s:
        return 'safe navigation operator `&.`'
    # --- keyword / forwarding args ----------------------------------------
    if '...' in s and ('def ' in s or '(' in s):
        return 'argument forwarding `...`'
    if re.search(r'\*\*\s*$|\*\*\)', s):
        return 'double-splat `**` in args'
    # --- numbered/it block params ----------------------------------------
    if re.search(r'\b_[1-9]\b', s):
        return 'numbered block params (`_1`)'
    # --- heredoc ----------------------------------------------------------
    if re.search(r'<<[~-]?["\'`]?\w', s):
        return 'heredoc literal (`<<~`, `<<-`, squiggly)'
    # --- percent literals -------------------------------------------------
    if re.search(r'%[wWiIqQrsx]?[\[({<|/!]', s):
        return 'percent literal (`%w`, `%i`, `%q`, `%r`, ...)'
    # --- label / kwargs in odd position ----------------------------------
    if 'label' in m:
        return 'symbol-key / label in unsupported position (`key:`)'
    # --- rescue/ensure modifier ------------------------------------------
    if re.search(r'\brescue\b', s) and 'unexpected' in m and 'rescue' in m:
        return 'inline `rescue` modifier / begin-less rescue'
    if 'ensure' in m and 'unexpected' in m:
        return '`ensure` in unsupported position'
    # --- BEGIN/END, __END__/DATA -----------------------------------------
    if re.match(r'(BEGIN|END)\b', s) or '__END__' in s:
        return 'BEGIN/END blocks or __END__/DATA'
    # --- string interpolation --------------------------------------------
    if 'interpolation' in m or 'strbeg' in m:
        return 'string interpolation / string literal edge case'
    # --- ternary / conditional -------------------------------------------
    if '?' in s and ':' in s and re.search(r'\?.*:', s):
        return 'ternary / conditional expression edge case'
    # --- splat / block-pass in odd position ------------------------------
    if re.search(r'[(,]\s*&', s) or re.search(r'[(,]\s*\*', s):
        return 'splat `*` / block-pass `&` in unsupported position'
    # --- fall-through by error token -------------------------------------
    tokm = re.search(r'\(([^)]+)\)\s*$', msg)
    if tokm:
        return f'unclassified (error token: {tokm.group(1)})'
    return 'unclassified'


def main():
    if len(sys.argv) != 3:
        print(__doc__)
        sys.exit(2)
    out_dir, repo = sys.argv[1], sys.argv[2]
    rbgo = os.path.join(out_dir, f'{repo}.rbgo.tsv')
    mri = os.path.join(out_dir, f'{repo}.mri.tsv')

    mri_valid = {}
    if os.path.exists(mri):
        for ln in open(mri):
            parts = ln.rstrip('\n').split('\t')
            if len(parts) >= 2:
                mri_valid[parts[1]] = (parts[0] == 'VALID')

    cats = {}       # category -> set(paths)
    examples = {}   # category -> [lines]
    for ln in open(rbgo):
        parts = ln.rstrip('\n').split('\t')
        kind, path = parts[0], parts[1]
        msg = parts[2] if len(parts) > 2 else ''
        if kind == 'OK':
            continue
        # only count real front-end gaps: MRI considers the file valid
        if mri_valid and not mri_valid.get(path, False):
            continue
        # extract offending line
        line = ''
        mln = re.search(r'line (\d+)', msg)
        if mln:
            try:
                with open(path, errors='replace') as f:
                    src = f.readlines()
                idx = int(mln.group(1)) - 1
                if 0 <= idx < len(src):
                    line = src[idx]
            except OSError:
                pass
        if kind == 'PANIC':
            cat = 'PANIC in front-end (parser/compiler crash)'
        elif kind == 'COMP':
            cat = 'compiler gap (parses, fails to lower): ' + msg.split(':')[-1].strip()[:50]
        else:
            cat = categorize(line, msg)
        cats.setdefault(cat, set()).add(path)
        ex = examples.setdefault(cat, [])
        if line.strip() and len(ex) < 4 and line.strip() not in ex:
            ex.append(line.strip())

    ranked = sorted(cats.items(), key=lambda kv: -len(kv[1]))
    total_gap = sum(len(v) for v in cats.values())
    print(f'## {repo}: {total_gap} real front-end gaps (rbgo rejects, MRI valid)\n')
    print(f'{"files":>6}  category')
    print(f'{"-"*6}  {"-"*60}')
    for cat, paths in ranked:
        print(f'{len(paths):>6}  {cat}')
    print()
    for cat, paths in ranked[:14]:
        print(f'### {len(paths)} files — {cat}')
        for ex in examples.get(cat, []):
            print(f'    {ex[:120]}')
        print()


if __name__ == '__main__':
    main()
