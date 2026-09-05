# Build stage.
FROM golang:1.25-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 is what makes the binary self-contained. The SQLite driver used
# here is pure Go precisely so this works: a cgo-linked driver would produce a
# build that succeeds and a container that fails to start.
RUN CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w" \
        -o /out/brewops .

# An empty directory to become the mount point. Docker initialises a new named
# volume from whatever the image has at that path, ownership included, so
# creating it here is what makes the volume writable by the nonroot user.
RUN mkdir -p /out/data

# Runtime stage.
#
# distroless/base rather than static: the shop's records live in a SQLite file,
# and this image ships with the directory ownership needed for a mounted volume
# to be writable by the nonroot user.
FROM gcr.io/distroless/base-debian12:nonroot

COPY --from=build /out/brewops /brewops
COPY --from=build --chown=nonroot:nonroot /out/data /data

# The database lives on a volume so the shop's records survive the container.
# Without one, every run starts from the seeded catalogue and forgets every
# extraction that was recorded.
VOLUME /data

USER nonroot:nonroot

# stdio transport: frames arrive on stdin and leave on stdout, so the container
# must be run with -i. Diagnostics go to stderr and never pollute the stream.
ENTRYPOINT ["/brewops", "-db", "/data/brewops.db"]
