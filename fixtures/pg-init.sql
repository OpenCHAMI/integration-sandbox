-- Initial schema bootstrap: per-service databases. SMD, boot, metadata, tokensmith, power, fru.
-- Each service owns its DB; password is the same as the cluster (dev only).
CREATE DATABASE smd;
CREATE DATABASE boot;
CREATE DATABASE metadata;
CREATE DATABASE tokensmith;
CREATE DATABASE power;
CREATE DATABASE fru;
