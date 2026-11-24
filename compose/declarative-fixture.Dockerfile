# Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
# SPDX-License-Identifier: MIT
FROM gcr.io/distroless/static-debian12:nonroot
COPY --chown=65532:65532 declarative-fixture /declarative-fixture
USER 65532:65532
ENTRYPOINT ["/declarative-fixture"]
