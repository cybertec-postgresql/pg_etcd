[![Coverage Status](https://coveralls.io/repos/github/cybertec-postgresql/etcd_fdw/badge.svg)](https://coveralls.io/github/cybertec-postgresql/etcd_fdw)

# etcd_fdw - Bidirectional Synchronization

## Overview

etcd_fdw provides bidirectional synchronization between etcd and PostgreSQL using a single table architecture with revision status encoding.

## Architecture

- **Single Table**: All data stored in `etcd` table with revision-based synchronization status
- **Revision Encoding**: `-1` = pending sync to etcd, `>0` = synchronized from etcd  
- **Polling Mechanism**: PostgreSQL to etcd sync uses configurable polling interval

## Installation

```bash
go install github.com/cybertec-postgresql/etcd_fdw/cmd/etcd_fdw@latest
```

## Usage

```bash
# Basic usage
etcd_fdw --postgres-dsn="postgres://user:pass@localhost/db" --etcd-dsn="etcd://localhost:2379/prefix"

# With custom polling interval
etcd_fdw --postgres-dsn="..." --etcd-dsn="..." --polling-interval=2s
```
