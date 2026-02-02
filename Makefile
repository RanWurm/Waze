BINARY_NAME=waze-server

run_server:
	go run cmd/server/main.go

run_sim:
	go run ./cmd/simulation
	

build:
	go build -o ${BINARY_NAME} cmd/server/main.go

clean:
	go clean
	rm ${BINARY_NAME}