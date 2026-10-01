# SPDX-FileCopyrightText: The Samoyed Authors
# SPDX-License-Identifier: GPL-2.0-or-later

import godeps

GO_LIST_OUTPUT = """
{
	"ImportPath": "github.com/doismellburning/samoyed/cmd/samoyed-q1test",
	"Imports": [
		"fmt",
		"github.com/doismellburning/samoyed/internal/direwolf",
		"github.com/doismellburning/samoyed/internal/maybe",
		"github.com/spf13/pflag"
	],
	"TestImports": ["github.com/doismellburning/samoyed/internal/testutils"]
}
{
	"ImportPath": "github.com/doismellburning/samoyed/internal/direwolf",
	"Imports": ["github.com/doismellburning/samoyed/internal/maybe"]
}
{
	"ImportPath": "github.com/doismellburning/samoyed/internal/maybe"
}
"""


def test_parse_go_list():
    packages = list(godeps.parse_go_list(GO_LIST_OUTPUT))
    assert [p["ImportPath"] for p in packages] == [
        "github.com/doismellburning/samoyed/cmd/samoyed-q1test",
        "github.com/doismellburning/samoyed/internal/direwolf",
        "github.com/doismellburning/samoyed/internal/maybe",
    ]


def test_packages_to_dot_keeps_only_direct_internal_non_test_imports():
    dot = godeps.packages_to_dot(godeps.parse_go_list(GO_LIST_OUTPUT))
    edges = sorted(line.strip() for line in dot.splitlines() if "->" in line)
    assert edges == [
        '"cmd/samoyed-q1test" -> "internal/direwolf";',
        '"internal/direwolf" -> "internal/maybe";',
    ]
    assert '"cmd/samoyed-q1test" [label="samoyed-q1test", shape=box];' in dot
    assert '"internal/maybe" [label="maybe", shape=ellipse];' in dot
    assert "testutils" not in dot
    assert "pflag" not in dot
    assert "subgraph" not in dot


def test_transitive_reduction():
    graph = {
        "a": {"b", "c", "d"},
        "b": {"c"},
        "c": {"d"},
        "d": set(),
        "e": {"d"},
    }
    assert godeps.transitive_reduction(graph) == {
        "a": {"b"},
        "b": {"c"},
        "c": {"d"},
        "d": set(),
        "e": {"d"},
    }


def test_transitive_reduction_keeps_imports_of_packages_outside_the_graph():
    assert godeps.transitive_reduction({"a": {"b", "c"}}) == {"a": {"b", "c"}}
