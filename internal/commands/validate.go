package commands

import (
	"encoding/json"
	"errors"
	"fmt"

	apiv1 "github.com/incidentflow/incidentflow-k8s-agent/pkg/api/v1"
)

func ValidateCommand(cmd apiv1.Command) error {
	if cmd.ID == "" {
		return errors.New("command id is required")
	}
	if cmd.Type != apiv1.MessageTypeCommand {
		return fmt.Errorf("unsupported message type %q", cmd.Type)
	}
	if cmd.Action == "" {
		return errors.New("command action is required")
	}
	if !IsAllowedAction(cmd.Action) {
		return fmt.Errorf("unsupported action %q", cmd.Action)
	}
	return nil
}

func decodeParams[T any](cmd apiv1.Command) (T, error) {
	var params T
	if len(cmd.Params) == 0 {
		return params, nil
	}
	if err := json.Unmarshal(cmd.Params, &params); err != nil {
		return params, err
	}
	return params, nil
}
