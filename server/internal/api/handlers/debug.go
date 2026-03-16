// Package handlers implements the HTTP handlers for the Rutoso REST API.
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/achetronic/rutoso/internal/api/respond"
	resourcev3 "github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// xdsResourceTypes lists the four core xDS resource types in the order they
// appear in a snapshot.
var xdsResourceTypes = []resourcev3.Type{
	resourcev3.ListenerType,
	resourcev3.ClusterType,
	resourcev3.RouteType,
	resourcev3.EndpointType,
}

// xdsResourceLabel maps each xDS type URL to a human-readable JSON key.
var xdsResourceLabel = map[resourcev3.Type]string{
	resourcev3.ListenerType: "listeners",
	resourcev3.ClusterType:  "clusters",
	resourcev3.RouteType:    "routes",
	resourcev3.EndpointType: "endpoints",
}

// GetXDSSnapshot returns the last xDS snapshot built by Rutoso as a JSON
// document. Each resource is serialised with protojson, producing the same
// field names and values that Envoy receives over ADS.
//
// The snapshot is available as soon as Rutoso starts — it does not require
// an Envoy instance to be connected. Compare this output against Envoy's
// GET :9901/config_dump to verify that what Rutoso pushes matches what
// Envoy has loaded.
//
// @Summary     Get xDS snapshot
// @Description Returns the last xDS snapshot pushed by Rutoso, serialised with protojson.
// @Tags        debug
// @Produce     json
// @Success     200  {object}  map[string]interface{}
// @Failure     404  {object}  respond.ErrorBody
// @Failure     500  {object}  respond.ErrorBody
// @Router      /debug/xds/snapshot [get]
func (d *Dependencies) GetXDSSnapshot(w http.ResponseWriter, r *http.Request) {
	snap := d.XDSServer.Snapshot()
	if snap == nil {
		respond.Error(w, http.StatusNotFound, "no snapshot available yet", d.Logger)
		return
	}

	marshaller := protojson.MarshalOptions{
		Multiline:       false,
		EmitUnpopulated: false,
		UseProtoNames:   true,
	}

	result := make(map[string][]json.RawMessage)

	for _, t := range xdsResourceTypes {
		label := xdsResourceLabel[t]
		for _, res := range snap.GetResources(string(t)) {
			msg, ok := res.(proto.Message)
			if !ok {
				continue
			}
			raw, err := marshaller.Marshal(msg)
			if err != nil {
				respond.Error(w, http.StatusInternalServerError, "marshalling resource: "+err.Error(), d.Logger)
				return
			}
			result[label] = append(result[label], json.RawMessage(raw))
		}
	}

	respond.JSON(w, http.StatusOK, result, d.Logger)
}
