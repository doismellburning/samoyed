..
    SPDX-FileCopyrightText: The Samoyed Authors
    SPDX-License-Identifier: GPL-2.0-or-later

Architecture
============

Package dependencies
--------------------

How Samoyed's own packages depend on one another - the commands under ``cmd/``
on the left, the shared packages under ``internal/`` to their right.  An arrow
points from a package to one it imports.  The standard library, third-party
packages and test-only imports are left out.

The graph is generated from ``go list`` every time the documentation is built,
so it always matches the code it was built from.

.. graphviz:: _generated/deps.dot
   :alt: Samoyed's internal package dependency graph
