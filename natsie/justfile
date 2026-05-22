default:
    @just --list

# build the natsie binary
build:
    @echo '{{ BOLD + CYAN }}Building natsie{{ NORMAL }}'
    go build -o natsie ./cmd/natsie

# pull the latest minor versions of dependencies
update:
    @cd ./cmd/natsie && go get -u

# run the test suite with race detector and coverage
test:
    go test -race -v ./... -covermode=atomic -coverprofile=coverage.out

# run golangci-lint
lint:
    golangci-lint run -c .golangci.yml

# tidy go.mod/go.sum
tidy:
    go mod tidy

# build the docker image (local; CI uses build-push-action)
docker:
    docker build -t ghcr.io/1995parham/natsie:dev .
