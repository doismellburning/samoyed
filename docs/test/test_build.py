# SPDX-FileCopyrightText: The Samoyed Authors
# SPDX-License-Identifier: GPL-2.0-or-later

import pathlib


def test_index_html_exists():
    assert (pathlib.Path(__file__).parent.parent / "build" / "dirhtml" / "index.html").exists()


def test_dependency_graph_rendered():
    images = pathlib.Path(__file__).parent.parent / "build" / "dirhtml" / "_images"
    assert list(images.glob("graphviz-*.svg"))
