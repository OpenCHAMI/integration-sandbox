# sushy-emulator config (loaded via SUSHY_EMULATOR_CONFIG).
#
# Give each --fake BMC sim a distinct EthernetInterface MAC (and, optionally, an
# IP). Identity is resolved at container startup in priority order:
#
#   1. SUSHY_NODE_IDX  -> MAC packed from the integer index.
#   2. SUSHY_NODE_MAC  -> use that MAC verbatim.
#   3. hostname -> stable hash of the full hostname, packed into the MAC.
#   4. nothing usable -> a random locally-administered MAC.
#
# The IP is included only when SUSHY_NODE_IP is set; otherwise the NIC advertises
# no IPv4 (null), matching stock sushy-tools. Without any of this every sim
# returns the same built-in MAC, which SMD rejects on its UNIQUE constraint.
import hashlib
import os
import random
import re
import socket


def _mac_from_index(idx):
    # Pack the index big-endian into the five octets after the 02: prefix
    # (02: = locally administered + unicast). The low octet fills first and
    # carries left, e.g. 1000 (0x3E8) -> 02:00:00:00:03:e8.
    return "02:%02x:%02x:%02x:%02x:%02x" % (
        (idx >> 32) & 0xFF,
        (idx >> 24) & 0xFF,
        (idx >> 16) & 0xFF,
        (idx >> 8) & 0xFF,
        idx & 0xFF,
    )


def _mac_from_hostname(host):
    # Stable per-hostname MAC: a cryptographic digest (NOT the builtin hash(),
    # which is salted per-process) folds the full hostname into the five octets
    # after the 02: prefix. Every distinct hostname maps to a distinct,
    # reproducible-across-restarts locally-administered MAC, so containers
    # started with different hostnames don't collide.
    digest = hashlib.blake2b(host.encode(), digest_size=5).digest()
    return "02:" + ":".join("%02x" % b for b in digest)


def _random_mac():
    # Random locally-administered unicast MAC (02: prefix guarantees both bits).
    return "02:%02x:%02x:%02x:%02x:%02x" % tuple(
        random.randint(0, 0xFF) for _ in range(5)
    )


_host = socket.gethostname()
_idx_env = os.environ.get("SUSHY_NODE_IDX")
_mac_env = os.environ.get("SUSHY_NODE_MAC")
_ip_env = os.environ.get("SUSHY_NODE_IP")

# Resolve the MAC by priority. `_generated` stays True whenever the MAC is
# deterministic (index / explicit / hostname hash) so the UUID can be derived
# from it; the random fallback flips it to False.
_generated = True
if _idx_env not in (None, ""):
    _mac = _mac_from_index(int(_idx_env))
elif _mac_env:
    _mac = _mac_env
elif _host:
    _mac = _mac_from_hostname(_host)
else:
    # Fallback: a random MAC, reached only when the container reports no
    # hostname. To use a fixed default instead, replace this with
    # e.g.  _mac = "02:00:00:00:00:00".
    _mac = _random_mac()
    _generated = False

# UUID: the MAC's 12 hex digits when we have a deterministic MAC, else a random
# 48-bit value. Rendered into the trailing group of a v4-shaped UUID.
if _generated:
    _uuid_hex = re.sub(r"[^0-9a-f]", "", _mac.lower()).rjust(12, "0")[-12:]
else:
    _uuid_hex = "%012x" % random.getrandbits(48)

_nic = {"mac": _mac}
if _ip_env:
    _nic["ip"] = _ip_env

SUSHY_EMULATOR_FAKE_SYSTEMS = [
    {
        "uuid": "00000000-0000-4000-8000-%s" % _uuid_hex,
        "name": _host,
        "power_state": "Off",
        "external_notifier": False,
        "nics": [_nic],
    }
]
