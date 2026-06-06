FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/api_proxy ./cmd/api_proxy

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/api_proxy /api_proxy
EXPOSE 8080
ENTRYPOINT ["/api_proxy"]
