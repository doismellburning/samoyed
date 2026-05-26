Replacing the Linux kernel AX.25 stack
======================================

Linux 7.1 removed the in-tree amateur radio subsystem — ``net/ax25/``,
``net/netrom/``, ``net/rose/``, ``drivers/net/hamradio/``, and the KISS and
6PACK tty line disciplines.
The ``AF_AX25``, ``AF_NETROM``, and ``AF_ROSE`` socket families went with it.
As a consequence, every binary in the Debian ``ax25-tools`` and ``ax25-apps``
packages — ``axcall``, ``axlisten``, ``kissattach``, ``ax25d``, ``beacon``,
``axparms``, and the rest — no longer works on a stock kernel.

This page audits each of those tools against what Samoyed already provides,
so users migrating off the kernel stack know which capabilities they still
have and which require new userspace code.

Each row carries one of three verdicts:

- **Covered** — Samoyed already does this; the column on the right points
  at the file or configuration knob.
- **Gap** — the capability is missing; a Samoyed-side replacement is
  outlined in the *Future work* section.
- **Not feasible** — the tool depended on a Linux kernel netdev or socket
  family that no longer exists; users need to switch to a different
  attachment model (AGW, KISS-PTY, or speaking KISS directly).

Core protocol coverage
----------------------

.. list-table::
   :header-rows: 1
   :widths: 35 15 50

   * - Capability
     - Verdict
     - Notes
   * - AX.25 v2.0 / v2.2 framing (HDLC, addresses, PID, FCS)
     - Covered
     - ``src/ax25_pad.go``, ``src/ax25_pad2.go``, ``src/hdlc_*.go``
   * - LAPB connected-mode state machine (SABM(E), I/RR/RNR/REJ, T1/T2/T3,
       mod-8 and mod-128, segmenter)
     - Covered
     - ``src/ax25_link.go``
   * - Digipeating (in-kernel AX.25 digi, ``rxecho``)
     - Covered
     - ``src/digipeater.go``, ``src/cdigipeater.go``; ``DIGIPEAT`` /
       ``CDIGIPEAT`` in ``direwolf.conf``
   * - NET/ROM L3 routing
     - Gap (in progress)
     - work in progress on the ``feat/netrom-transport`` branch; not yet
       merged
   * - ROSE (X.25 over AX.25)
     - Out of scope
     - no implementation, no plans — ROSE users should look elsewhere

Attachment / transport
----------------------

.. list-table::
   :header-rows: 1
   :widths: 35 15 50

   * - Tool / capability
     - Verdict
     - Notes
   * - KISS over TCP
     - Covered
     - ``src/kissnet.go``; ``KISSPORT`` in ``direwolf.conf``
   * - KISS over serial / Bluetooth ``rfcomm``
     - Covered
     - ``src/kissserial.go``; ``SERIALKISS`` / ``SERIALKISSPOLL``
   * - KISS over a pseudo-tty
     - Covered (partial)
     - ``src/kiss.go`` creates ``/tmp/kisstnc`` symlinked to the slave PTY.
       Some line-discipline settings are still ``TODO``
   * - AGW protocol (the AGWPE TCP interface)
     - Covered
     - ``src/server.go``; default port 8000.
       Supports connected-mode commands ``C`` / ``v`` / ``c`` / ``D`` / ``d``
   * - ``kissattach`` — attach a tty as an ``axN`` netdev
     - Not feasible
     - the kernel netdev no longer exists.
       Use AGW or open Samoyed's KISS-PTY (``/tmp/kisstnc``) directly
   * - ``mkiss`` / ``m6pack`` — multi-port KISS demux
     - Covered
     - ``samoyed-direwolf`` handles multiple KISS ports natively
   * - ``kissparms`` — per-port TXdelay / persist / slot
     - Covered
     - the AGW KISS commands ``_1``..``_4`` set these on the live TNC
   * - ``ax25ipd`` — AX.25 over UDP encapsulation
     - Gap (deferred)
     - no implementation in any branch

Connected-mode clients and daemons
----------------------------------

.. list-table::
   :header-rows: 1
   :widths: 35 15 50

   * - Tool
     - Verdict
     - Notes
   * - ``axcall`` / ``call`` — interactive outbound connect
     - Gap
     - tracked below as ``samoyed-axcall``.
       The half-built ``agwlib_C_connect`` already lives in
       ``cmd/samoyed-appserver/agwlib.go`` (currently
       ``//nolint:unused``)
   * - ``axlisten`` — frame monitor / decoder
     - Gap
     - tracked below as ``samoyed-axlisten``.
       The AGW ``k`` (raw) and ``m`` (monitor) feeds already exist;
       what is missing is a small CLI wrapper
   * - ``ax25d`` — inetd-for-AX.25 inbound dispatcher
     - Gap
     - tracked below as ``samoyed-ax25d``.
       ``cmd/samoyed-appserver/`` shows the pattern but is example code,
       not a config-driven daemon
   * - ``axctl`` — kick a connection
     - Covered
     - send AGW ``d`` (or invoke it from the future ``samoyed-axcall``
       on a running session)
   * - ``axspawn`` — login handler
     - Not feasible
     - depended on the kernel's ``AF_AX25`` accept loop.
       An equivalent has to be rebuilt on top of an AGW dispatcher
   * - ``axwrapper`` — stdio-as-socket wrapper
     - Not feasible
     - same reason as ``axspawn``

Beacons, monitoring, and bookkeeping
------------------------------------

.. list-table::
   :header-rows: 1
   :widths: 35 15 50

   * - Tool
     - Verdict
     - Notes
   * - ``beacon`` — periodic UI frame
     - Covered
     - ``PBEACON`` / ``OBEACON`` / ``IBEACON`` / ``CBEACON`` in
       ``direwolf.conf``; ``src/beacon.go``
   * - ``mheard`` / ``mheardd`` — recently-heard station list
     - Gap
     - tracked below as ``samoyed-mheard``.
       The raw data is already written to ``direwolf.log``
   * - ``axgetput`` / ``bget`` / ``bput`` — YAPP file transfer
     - Gap (deferred)
     - no implementation in any branch
   * - ``axparms`` — kernel callsign / uid map, route table, port params
     - Not feasible (partial)
     - the kernel-state parts no longer apply.
       Equivalents that still make sense live in ``direwolf.conf``

Future work
-----------

The audit found several gaps to be sized and addressed separately:

#. **samoyed-axcall** — interactive outbound connect client.
   MVP: line-mode terminal with tilde escapes for disconnect, help, and
   reconnect.
   YAPP / 7plus file transfer and curses splitscreen are deliberately out
   of MVP scope.
#. **samoyed-axlisten** — frame monitor CLI wrapping the AGW ``k``/``m``
   feeds.
   Existing decoders in ``src/`` can be reused for the human-readable
   output.
#. **samoyed-ax25d** — real, config-driven inbound-connect dispatcher
   daemon (the role ``ax25d`` filled with ``ax25d.conf``).
   ``cmd/samoyed-appserver/`` is the starting point.
#. **samoyed-mheard** — heard-list utility, sourced from ``direwolf.log``.
#. **YAPP file transfer** — the protocol behind ``axgetput`` / ``bget`` /
   ``bput``.
   Larger scope; deferred.
#. **AX.25-over-UDP** — replacement for ``ax25ipd``, deferred.
#. **Prerequisite refactor:** the AGW client helpers (``agwlib_init``,
   ``agwlib_C_connect``, ``agwlib_d_disconnect``,
   ``agwlib_D_send_connected_data``, ``agwlib_X_register_callsign``,
   ``agwlib_Y_outstanding_frames_for_station``) currently live in
   ``cmd/samoyed-appserver/agwlib.go`` as ``package main``.
   They need to be lifted into an ``internal/agwlib/`` package before a
   second AGW-speaking tool (axcall, axlisten, mheard) can share them.
#. **Document the KISS-PTY attachment path** — now that ``kissattach`` is
   gone, ``/tmp/kisstnc`` and the AGW port are the two supported
   attachment points; users coming from the kernel stack need an explicit
   walk-through of either one.

NET/ROM is already in flight on the ``feat/netrom-transport`` branch and is
not reopened here.

ROSE is out of scope.
