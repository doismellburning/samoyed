Protocol Support Notes
======================

INP3
----

`INP3 <https://www.cantab.net/users/john.wiseman/Documents/BPQCFGFile.html>`__ is a routing / link-metric
extension to NET/ROM used by G8BPQ's BPQ32/LinBPQ node software and compatible node switches (TheNet-X,
XRouter). Samoyed recognises INP3 frames (routing updates, link-timing probes, and keepalives, all carried
inside the existing NET/ROM PID 0xCF pass-through) and prints a human-readable decode of them, the same way
it does for APRS traffic.

Samoyed does **not** participate in INP3 routing - it does not maintain a routing table, probe neighbours,
or broadcast its own presence, and it never forwards connected-mode sessions on behalf of other nodes. That
would require a full NET/ROM Level 4 (connected-mode circuit-switching) implementation, which Samoyed - like
Dire Wolf - does not have. INP3 decoding here is purely for visibility: it lets an operator see INP3 traffic
on the air in a readable form instead of as opaque binary bytes.
