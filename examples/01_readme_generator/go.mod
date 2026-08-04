module github.com/hyperparallel/examples/readme_generator

go 1.21

require github.com/hyperparallel/runtime v0.0.0

require (
	github.com/gorilla/websocket v1.5.3 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/hyperparallel/runtime => ../../runtime
