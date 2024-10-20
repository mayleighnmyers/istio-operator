package kube

import (
	"encoding/json"

	"gomodules.xyz/jsonpatch/v2"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type StatusPatch struct {
	status interface{}
}

func NewStatusPatch(status interface{}) *StatusPatch {
	return &StatusPatch{
		status: status,
	}
}

func (p *StatusPatch) Type() types.PatchType {
	return types.JSONPatchType
}

func (p *StatusPatch) Data(obj client.Object) ([]byte, error) {
	data := []jsonpatch.Operation{
		{
			Operation: "replace",
			Path:      "/status",
			Value:     p.status,
		},
	}
	statusJSON, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return statusJSON, nil
}
