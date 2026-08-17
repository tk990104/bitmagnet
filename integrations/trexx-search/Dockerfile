FROM golang:1.23-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
COPY integrations/trexx-search ./integrations/trexx-search
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /trexx-search ./integrations/trexx-search

FROM alpine:3.21
RUN addgroup -S trexx && adduser -S -G trexx trexx
COPY --from=build /trexx-search /usr/local/bin/trexx-search
USER trexx
EXPOSE 8787
ENV TREXX_ADDR=0.0.0.0:8787
ENTRYPOINT ["/usr/local/bin/trexx-search"]
