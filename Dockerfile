FROM golang:1.22-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
COPY vendor ./vendor
RUN go list -mod=vendor all >/dev/null
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -buildvcs=false -o /out/hawkeye ./cmd/server

FROM alpine:3.20

RUN adduser -D -H hawkeye
WORKDIR /app
COPY --from=build /out/hawkeye /app/hawkeye
RUN mkdir -p /app/uploads/avatars /app/config && chown -R hawkeye:hawkeye /app
USER hawkeye
EXPOSE 8080
CMD ["/app/hawkeye"]
