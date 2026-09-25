# Patched sushy-tools for the BMC sims.
#
# The stock `--fake` driver advertises every node with the same built-in MAC
# (00:5c:52:31:3a:9c) and a null EthernetInterface IP: FakeDriver.get_nics()
# returns only {id, mac} and drops the configured nic 'ip'. magellan collects
# IP="" and SMD then rejects the RedfishEndpoint POST with
# "Invalid CompEthInterface IP Address" (empty IP) / a UNIQUE-constraint
# collision (duplicate MAC).
#
# This image keeps the fake driver (so UC6 power actions still work) but:
#   1. surfaces the configured nic 'ip' from get_nics(), and
#   2. renders it as IPv4Addresses in the ethernet_interface template,
# and bakes a config that gives each sim a unique MAC/IP derived from its
# hostname (the xname alias). Result: magellan -> SMD ingestion succeeds with
# valid, unique per-node CompEthInterfaces.
ARG BASE=quay.io/metal3-io/sushy-tools:latest
FROM ${BASE}

COPY ethernet_interface.json /tmp/ethernet_interface.json
COPY config.py /etc/sushy/config.py

# Surgical patch against the installed package; fail loudly if the upstream
# shape drifts so the breakage is caught at build time, not at runtime.
RUN set -eux; \
    d="$(python3 -c 'import os, sushy_tools; print(os.path.dirname(sushy_tools.__file__))')"; \
    sed -i "s/'id': nic.get('mac'), 'mac': nic.get('mac')}/'id': nic.get('mac'), 'mac': nic.get('mac'), 'ip': nic.get('ip')}/" \
        "$d/emulator/resources/systems/fakedriver.py"; \
    grep -q "'ip': nic.get('ip')" "$d/emulator/resources/systems/fakedriver.py"; \
    cp /tmp/ethernet_interface.json "$d/emulator/templates/ethernet_interface.json"; \
    grep -q IPv4Addresses "$d/emulator/templates/ethernet_interface.json"

ENV SUSHY_EMULATOR_CONFIG=/etc/sushy/config.py
