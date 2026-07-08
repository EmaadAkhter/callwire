package callwire

import "github.com/vmihailenco/msgpack/v5"

type wireMessage struct {
	ID        uint64        `msgpack:"id"`
	Type      string        `msgpack:"type"`
	Func      string        `msgpack:"func,omitempty"`
	Args      []interface{} `msgpack:"args"`
	Stream    bool          `msgpack:"stream,omitempty"`
	Result    interface{}   `msgpack:"result,omitempty"`
	ErrorType string        `msgpack:"error_type,omitempty"`
	Message   string        `msgpack:"message,omitempty"`
}

func EncodeRequest(id uint64, funcName string, args []interface{}) ([]byte, error) {
	return msgpack.Marshal(wireMessage{ID: id, Type: "request", Func: funcName, Args: args})
}

func encodeRequest(id uint64, funcName string, args []interface{}) ([]byte, error) {
	return EncodeRequest(id, funcName, args)
}

func encodeStreamRequest(id uint64, funcName string, args []interface{}) ([]byte, error) {
	return msgpack.Marshal(wireMessage{ID: id, Type: "stream_request", Func: funcName, Args: args})
}

func encodeResponse(id uint64, result interface{}) ([]byte, error) {
	return msgpack.Marshal(wireMessage{ID: id, Type: "response", Result: result})
}

func encodeError(id uint64, errorType, message string) ([]byte, error) {
	return msgpack.Marshal(wireMessage{ID: id, Type: "error", ErrorType: errorType, Message: message})
}

func encodeStreamChunk(id uint64, result interface{}) ([]byte, error) {
	return msgpack.Marshal(wireMessage{ID: id, Type: "stream_chunk", Result: result})
}

func encodeStreamEnd(id uint64) ([]byte, error) {
	return msgpack.Marshal(wireMessage{ID: id, Type: "stream_end"})
}

func encodeStreamClose(id uint64) ([]byte, error) {
	return msgpack.Marshal(wireMessage{ID: id, Type: "stream_close"})
}

func encodeBidiRequest(id uint64, funcName string, args []interface{}) ([]byte, error) {
	return msgpack.Marshal(wireMessage{ID: id, Type: "request", Func: funcName, Args: args, Stream: true})
}

func decodeMessage(payload []byte) (wireMessage, error) {
	var msg wireMessage
	err := msgpack.Unmarshal(payload, &msg)
	return msg, err
}
