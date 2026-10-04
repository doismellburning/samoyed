# SPDX-FileCopyrightText: The Samoyed Authors
# SPDX-License-Identifier: AGPL-3.0-or-later

"""Generate a Graphviz graph of Samoyed's internal package dependencies.

On every build this asks ``go list`` for the module's packages and writes a DOT
file that ``sphinx.ext.graphviz`` then renders. Only imports within the module
are kept - the standard library and third-party packages would swamp it.
"""

import json
import pathlib
import subprocess
from collections.abc import Iterable, Iterator
from typing import Any

from sphinx.application import Sphinx
from sphinx.errors import ExtensionError

MODULE = "github.com/doismellburning/samoyed"
REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
OUTPUT = pathlib.Path("_generated") / "deps.dot"


def parse_go_list(output: str) -> Iterator[dict[str, Any]]:
    """Parse ``go list -json``'s output, a stream of concatenated JSON objects."""
    decoder = json.JSONDecoder()
    index = 0
    while True:
        while index < len(output) and output[index].isspace():
            index += 1
        if index == len(output):
            return
        package, index = decoder.raw_decode(output, index)
        yield package


def _short(import_path: str) -> str:
    return import_path.removeprefix(MODULE + "/")


def transitive_reduction(graph: dict[str, set[str]]) -> dict[str, set[str]]:
    """Drop each edge that a longer path already implies.

    If ``a`` imports both ``b`` and ``c``, and ``b`` depends on ``c``, then
    ``a -> c`` says nothing the graph doesn't already show, so it goes. Go
    forbids import cycles, so the graph is acyclic and the reduction unique.
    """
    reachable: dict[str, set[str]] = {}

    def descendants(node: str) -> set[str]:
        if node not in reachable:
            found: set[str] = set()
            for dep in graph.get(node, ()):
                found.add(dep)
                found |= descendants(dep)
            reachable[node] = found
        return reachable[node]

    return {
        node: {dep for dep in deps if not any(dep in descendants(other) for other in deps - {dep})}
        for node, deps in graph.items()
    }


def packages_to_dot(packages: Iterable[dict[str, Any]]) -> str:
    """Render the module's packages and their in-module imports as DOT.

    ``Imports`` excludes test-only imports, so test helpers don't appear as
    dependencies. Imports already implied by another path are left out, as
    are clusters for ``cmd`` and ``internal``: both pull edges into long
    detours around the cluster boxes, and the node shapes tell the two apart.
    """
    prefix = MODULE + "/"
    nodes: dict[str, set[str]] = {}
    for package in packages:
        path = package["ImportPath"]
        if not path.startswith(prefix):
            continue
        nodes[_short(path)] = {_short(i) for i in package.get("Imports", []) if i.startswith(prefix)}
    nodes = transitive_reduction(nodes)

    lines = [
        "digraph samoyed {",
        "    rankdir=LR;",
        '    node [fontname="sans-serif", fontsize=10];',
        "    edge [color=gray40];",
    ]
    for group, shape in (("cmd", "box"), ("internal", "ellipse")):
        lines.extend(
            f'    "{n}" [label="{n.removeprefix(group + "/")}", shape={shape}];'
            for n in sorted(nodes)
            if n.startswith(group + "/")
        )
    for node in sorted(nodes):
        lines.extend(f'    "{node}" -> "{dep}";' for dep in sorted(nodes[node]))
    lines.append("}")
    return "\n".join(lines) + "\n"


def generate(app: Sphinx) -> None:
    try:
        result = subprocess.run(
            ["go", "list", "-json", "./..."],
            cwd=REPO_ROOT,
            capture_output=True,
            text=True,
            check=True,
        )
    except FileNotFoundError as e:
        raise ExtensionError("godeps: `go` not found - it's needed to generate the dependency graph") from e
    except subprocess.CalledProcessError as e:
        raise ExtensionError(f"godeps: `go list` failed:\n{e.stderr}") from e

    dot = packages_to_dot(parse_go_list(result.stdout))
    target = pathlib.Path(app.srcdir) / OUTPUT
    if not target.exists() or target.read_text() != dot:
        target.parent.mkdir(exist_ok=True)
        target.write_text(dot)


def setup(app: Sphinx) -> dict[str, Any]:
    app.connect("builder-inited", generate)
    return {"parallel_read_safe": True, "parallel_write_safe": True}
