package routing

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestComfyCheckpointsReadsBothComfyUIFormats(t *testing.T) {
	cases := map[string]struct {
		body string
		want []string
		ok   bool
	}{
		"legacy":      {`{"CheckpointLoaderSimple":{"input":{"required":{"ckpt_name":[["sdxl.safetensors"]]}}}}`, []string{"sdxl.safetensors"}, true},
		"combo":       {`{"CheckpointLoaderSimple":{"input":{"required":{"ckpt_name":["COMBO",{"options":["flux.safetensors"]}]}}}}`, []string{"flux.safetensors"}, true},
		"not comfyui": {`{"status":"ok"}`, nil, false},
	}
	for name, tc := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(tc.body))
		}))
		got, ok := comfyCheckpoints(server.Client(), server.URL)
		server.Close()
		if ok != tc.ok || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, %v; want %v, %v", name, got, ok, tc.want, tc.ok)
		}
	}
}
