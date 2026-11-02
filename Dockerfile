# Multi-stage, cross-compiled: the build stage runs on the runner's native arch and Go
# emits the target arch, so linux/arm64 builds need no QEMU. x360-common comes from vendor/,
# so the build needs nothing outside this repository.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -mod=vendor -trimpath -ldflags="-s -w" -o /out/x360-xmessaging ./cmd/x360-xmessaging

FROM alpine:3.22
RUN addgroup -S app && adduser -S -G app app
COPY --from=build /out/x360-xmessaging /usr/local/bin/x360-xmessaging
USER app
EXPOSE 10018/tcp 10118/tcp
ENTRYPOINT ["/usr/local/bin/x360-xmessaging"]
