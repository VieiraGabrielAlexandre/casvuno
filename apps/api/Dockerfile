# syntax=docker/dockerfile:1
FROM golang:1.27.3-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags="-s -w" -o /out/casvuno ./cmd/casvuno

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/casvuno /casvuno
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/casvuno"]
CMD ["serve"]
