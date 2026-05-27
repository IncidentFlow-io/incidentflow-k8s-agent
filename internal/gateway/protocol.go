package gateway

import (
	"encoding/json"
	"fmt"

	apiv1 "github.com/incidentflow/incidentflow-k8s-agent/pkg/api/v1"
)

func DecodeCommand(data []byte) (apiv1.Command, error) {
	var cmd apiv1.Command
	if err := json.Unmarshal(data, &cmd); err != nil {
		return apiv1.Command{}, err
	}
	if cmd.Type != apiv1.MessageTypeCommand {
		return apiv1.Command{}, fmt.Errorf("unsupported gateway message type %q", cmd.Type)
	}
	return cmd, nil
}

func EncodeResponse(resp apiv1.Response) ([]byte, error) {
	return json.Marshal(resp)
}
