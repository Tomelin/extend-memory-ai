FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/memory-api ./cmd/memory-api \
 && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/memory-admin ./cmd/memory-admin

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/memory-api /out/memory-admin /usr/local/bin/
EXPOSE 8080 9090
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/memory-api"]
