# Configuration file for the Sphinx documentation builder.
#
# For the full list of built-in configuration values, see the documentation:
# https://www.sphinx-doc.org/en/master/usage/configuration.html

import pathlib
import sys

from docutils import nodes
from sphinx.application import Sphinx

sys.path.insert(0, str(pathlib.Path(__file__).parent / "_ext"))

# -- Project information -----------------------------------------------------
# https://www.sphinx-doc.org/en/master/usage/configuration.html#project-information

project = "Samoyed"
copyright = "2026, The Samoyed Authors"
author = "The Samoyed Authors"
language = "en"

# -- General configuration ---------------------------------------------------
# https://www.sphinx-doc.org/en/master/usage/configuration.html#general-configuration

extensions = [
    "sphinx.ext.autosectionlabel",
    "sphinx.ext.extlinks",
    "sphinx.ext.graphviz",
    "godeps",
    "sphinx_eu_ai_label",
    "sphinx_last_updated_by_git",
    "sphinx_llm.txt",
    "sphinx_sitemap",
    "sphinxext.opengraph",
    "sphinxext.rediraffe",
]

# Label sections "page:Heading" rather than "Heading", so different pages can
# share subheadings without clashing.
autosectionlabel_prefix_document = True

graphviz_output_format = "svg"

pygments_style = "default"

templates_path = ["_templates"]
exclude_patterns = ["_ext", "_generated"]


# -- Options for HTML output -------------------------------------------------
# https://www.sphinx-doc.org/en/master/usage/configuration.html#options-for-html-output

html_theme = "sphinx_rtd_theme"
html_static_path = ["_static"]
html_logo = "_static/logo.png"
html_favicon = "_static/favicon.png"
html_show_sourcelink = False

# Where the site is published (by the docs-deploy job), for the sitemap,
# OpenGraph tags and llms.txt to give absolute URLs.
html_baseurl = "https://doismellburning.github.io/samoyed/"

# -- Extension configuration -------------------------------------------------

# The site is one unversioned language, so leave sphinx_sitemap's default
# "{lang}{version}" prefix off the URLs.
sitemap_url_scheme = "{link}"

# sphinx_llm's Markdown builder has no translator for graphviz diagrams, so they
# are left out of the llms.txt Markdown rather than failing the build.
llms_txt_suppress_unknown_node_warnings = ["graphviz"]

llms_txt_description = (
    "Samoyed is a soundcard modem, software TNC and NET/ROM node for amateur packet radio,"
    " written in Go, that started as a port of Dire Wolf."
)

ogp_site_url = html_baseurl
ogp_image = "_static/logo.png"

# Old page name -> new page name, so moving or renaming a page leaves a redirect
# behind instead of a broken link.
rediraffe_redirects: dict[str, str] = {
    "architecture": "internals/architecture",
    "architecture/receive-path": "internals/architecture/receive-path",
    "architecture/wiring": "internals/architecture/wiring",
}


def _drop_comments_from_markdown(app: Sphinx, doctree: nodes.document, docname: str) -> None:
    """Leave reST comments (every page's SPDX header) out of the llms.txt Markdown.

    sphinx_llm's Markdown builder writes them out as HTML comments, which its
    llms.txt index then takes for the start of each page's description.
    """
    if app.builder.name != "llms-markdown":
        return
    for comment in list(doctree.findall(nodes.comment)):
        comment.parent.remove(comment)


def setup(app: Sphinx) -> None:
    app.connect("doctree-resolved", _drop_comments_from_markdown)
