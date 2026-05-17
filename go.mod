module github.com/byte-v-forge/browser-automation

go 1.26

replace github.com/byte-v-forge/contracts => ../contracts

require (
	github.com/byte-v-forge/contracts v0.0.0-00010101000000-000000000000
	google.golang.org/grpc v1.81.1
	google.golang.org/protobuf v1.36.11
)

require (
	golang.org/x/net v0.53.0 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.36.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260226221140-a57be14db171 // indirect
)
