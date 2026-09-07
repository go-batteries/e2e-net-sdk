module github.com/go-batteries/e2e-net-sdk/example/lifecycle

go 1.24.0

require (
	github.com/go-batteries/e2e-net-sdk/myaccount/ec2 v0.0.0
	github.com/go-batteries/e2e-net-sdk/myaccount/iam v0.0.0
)

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/oapi-codegen/runtime v1.7.0 // indirect
)

replace github.com/go-batteries/e2e-net-sdk/myaccount/ec2 => ../../myaccount/ec2

replace github.com/go-batteries/e2e-net-sdk/myaccount/iam => ../../myaccount/iam
