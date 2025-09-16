# When building an image it is recommended to provide version arguments, e.g.
# docker build --no-cache -t cybertecpostgresql/pg_timetable:<tagname> \
#     --build-arg COMMIT=`git show -s --format=%H HEAD` \
#     --build-arg VERSION=`git describe --tags --abbrev=0` \
#     --build-arg DATE=`git show -s --format=%cI HEAD` .
FROM golang:alpine AS builder

ARG LDFLAGS

# Set necessary environmet variables needed for our image
ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64

# Move to working directory /build
WORKDIR /build

# Copy and download dependency using go mod
COPY go.mod .
COPY go.sum .
RUN go mod download

# Copy the code into the container
COPY . .

# Build the application
RUN go build -buildvcs=false \
-ldflags="$LDFLAGS" \
-o pg_etcd \
./cmd/pg_etcd

# Update certificates
RUN apk update && apk upgrade && apk add --no-cache ca-certificates
RUN update-ca-certificates

FROM alpine

# Copy the binary and certificates into the container
COPY --from=builder /build/pg_etcd /
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Command to run the executable
ENTRYPOINT ["/pg_etcd"]
