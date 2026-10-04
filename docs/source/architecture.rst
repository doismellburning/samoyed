..
    SPDX-FileCopyrightText: The Samoyed Authors
    SPDX-License-Identifier: AGPL-3.0-or-later

Architecture
============

Package dependencies
--------------------

How Samoyed's own packages depend on one another - the commands under ``cmd/``
are boxes, the shared packages under ``internal/`` ellipses.  An arrow points
from a package to one it depends on.  An import that another path already
implies is left out to keep the graph legible: if ``a`` imports ``b`` and
``c``, and ``b`` imports ``c``, only ``a -> b -> c`` is drawn.  The standard
library, third-party packages and test-only imports are left out too.

The graph is generated from ``go list`` every time the documentation is built,
so it always matches the code it was built from.

.. graphviz:: _generated/deps.dot
   :alt: Samoyed's internal package dependency graph
