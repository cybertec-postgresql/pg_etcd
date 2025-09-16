[![Coverage Status](https://coveralls.io/repos/github/cybertec-postgresql/etcd_fdw/badge.svg)](https://coveralls.io/github/cybertec-postgresql/etcd_fdw)

# pg_etcd - Bidirectional Synchronization

## Overview

pg_etcd provides bidirectional synchronization between etcd and PostgreSQL using a single table architecture with revision status encoding.

## Architecture

- **Single Table**: All data stored in `etcd` table with revision-based synchronization status
- **Revision Encoding**: `-1` = pending sync to etcd, `>0` = synchronized from etcd  
- **Polling Mechanism**: PostgreSQL to etcd sync uses configurable polling interval

## Installation

```bash
go install github.com/cybertec-postgresql/pg_etcd/cmd/pg_etcd@latest
```

## Usage

```bash
# Basic usage
pg_etcd --postgres-dsn="postgres://user:pass@localhost/db" --etcd-dsn="etcd://localhost:2379/prefix"

# With custom polling interval
pg_etcd --postgres-dsn="..." --etcd-dsn="..." --polling-interval=2s
```
