# Issue 6 filesystem fault probe

This manual probe runs the real cache store against task-owned Linux filesystems and refuses a passing result unless a forced cache replacement returns the expected storage error while preserving the prior snapshot bytes.

Build the Linux arm64 probe with the repository's pinned toolchain.

```sh
mkdir -p .artifacts/issue6-faults
GOCACHE="$PWD/.artifacts/issue6-faults/gocache" GOOS=linux GOARCH=arm64 CGO_ENABLED=0 mise exec -- go build -trimpath -o .artifacts/issue6-faults/fault-probe ./internal/cache/testdata/faultprobe
```

Use only the existing Ubuntu image `sha256:33ceb71981b602c1a7443a53469e4dba065f7503eab3078a2d7a57a2ab987517` on `linux/arm64`.

```sh
docker volume create remainder-issue6-faultprobe-bin
docker volume create remainder-issue6-faultprobe-cache
docker create --name remainder-issue6-faultprobe-stage --network none --mount type=volume,src=remainder-issue6-faultprobe-bin,dst=/probe sha256:33ceb71981b602c1a7443a53469e4dba065f7503eab3078a2d7a57a2ab987517 /bin/bash -lc 'true'
docker cp .artifacts/issue6-faults/fault-probe remainder-issue6-faultprobe-stage:/probe/fault-probe
docker rm remainder-issue6-faultprobe-stage
```

Seed a task-owned cache volume, then run the forced replacement through a read-only root and a read-only cache mount.

```sh
docker run --rm --network none --read-only --tmpfs /tmp:rw,size=16m --mount type=volume,src=remainder-issue6-faultprobe-bin,dst=/probe,readonly --mount type=volume,src=remainder-issue6-faultprobe-cache,dst=/cache sha256:33ceb71981b602c1a7443a53469e4dba065f7503eab3078a2d7a57a2ab987517 /probe/fault-probe seed
docker run --rm --network none --read-only --tmpfs /tmp:rw,size=16m --mount type=volume,src=remainder-issue6-faultprobe-bin,dst=/probe,readonly --mount type=volume,src=remainder-issue6-faultprobe-cache,dst=/cache,readonly sha256:33ceb71981b602c1a7443a53469e4dba065f7503eab3078a2d7a57a2ab987517 /probe/fault-probe readonly
```

The read-only run must print `RESULT=readonly-storage-unavailable-old-record-preserved` and the same snapshot SHA256 as the seed run.

Run the full-disk replacement on a task-owned bounded tmpfs.

```sh
docker run --rm --network none --read-only --tmpfs /tmp:rw,size=16m --tmpfs /cache:rw,size=512k --mount type=volume,src=remainder-issue6-faultprobe-bin,dst=/probe,readonly sha256:33ceb71981b602c1a7443a53469e4dba065f7503eab3078a2d7a57a2ab987517 /probe/fault-probe full
```

The full-disk run must print `RESULT=full-storage-unavailable-old-record-preserved` after it observes `ENOSPC` from the real Linux tmpfs and confirms the snapshot SHA256 is unchanged.

Remove only these named task-owned Docker volumes after capturing their output.

```sh
docker volume rm remainder-issue6-faultprobe-bin remainder-issue6-faultprobe-cache
```
