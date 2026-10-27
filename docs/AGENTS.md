# AGENTS.md

* Mark a page you write with `.. ai-label:: generated` under its title (from [sphinx-eu-ai-label](https://github.com/doismellburning/sphinx-eu-ai-label)), rather than a 🤖 prefix. Everything here goes through human review, but review isn't rewriting: a page whose words are largely an agent's is still generated
* Don't relabel a page you've only edited, and don't use `modified` - the index page's site-wide `.. ai-label:: basic` with `:text: Parts of this site were written with` already covers AI's hand in the rest
* Every claim needs a source in the page's References; drop what you can't source rather than leaving it in
* Don't say what Samoyed supports - anything documented here is assumed to be something it works with
* Link a cited standard where it's named, e.g. :rfc:`1226`
* Leave out context that doesn't help the reader understand the term
* `uv run sphinx-build -W -b html source <outdir>` builds the docs and fails on warnings
