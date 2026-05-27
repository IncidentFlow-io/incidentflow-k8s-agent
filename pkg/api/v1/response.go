package v1

const (
	StatusSuccess = "success"
	StatusError   = "error"
)

type Response struct {
	ID     string     `json:"id"`
	Type   string     `json:"type"`
	Status string     `json:"status"`
	Data   any        `json:"data,omitempty"`
	Error  *ErrorBody `json:"error,omitempty"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func Success(id string, data any) Response {
	return Response{ID: id, Type: MessageTypeResponse, Status: StatusSuccess, Data: data}
}

func Failure(id string, code string, message string) Response {
	return Response{
		ID:     id,
		Type:   MessageTypeResponse,
		Status: StatusError,
		Error:  &ErrorBody{Code: code, Message: message},
	}
}
