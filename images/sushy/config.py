# sushy-emulator config (loaded via SUSHY_EMULATOR_CONFIG).
#
# Give each --fake BMC sim a distinct EthernetInterface. The MAC/IP are derived
# from the container hostname (the xname alias, e.g. x0c0s3b0) so all 8 sims
# advertise unique, valid data. Without this every sim returns the same MAC and
# a null IP, which SMD rejects.
import re
import socket

_host = socket.gethostname()
_match = re.search(r"s(\d+)b\d+", _host)
_idx = int(_match.group(1)) if _match else 0

SUSHY_EMULATOR_FAKE_SYSTEMS = [
    {
        "uuid": "00000000-0000-4000-8000-%012x" % _idx,
        "name": _host,
        "power_state": "Off",
        "external_notifier": False,
        "nics": [
            {
                "mac": "02:00:00:00:00:%02x" % _idx,
                "ip": "10.0.0.%d" % (_idx + 1),
            }
        ],
    }
]
