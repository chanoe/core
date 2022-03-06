package main

import (
	"context"
	"fmt"
	"time"

	v1 "github.com/tkeel-io/core/api/core/v1"
	"github.com/tkeel-io/core/pkg/util"
	xkafka "github.com/tkeel-io/core/pkg/util/kafka"
)

func main() {
	sinks := []string{
		"kafka://139.198.125.147:9092/core0/core",
		"kafka://139.198.125.147:9092/core1/core",
	}

	stopCh := make(chan struct{}, 0)
	for _, sink := range sinks {
		sinkIn, err := xkafka.NewKafkaPubsub(sink)
		if nil != err {
			panic(err)
		}
		go func() {
			for {
				entityID := util.UUID("en-")
				sinkIn.Send(context.Background(), &v1.ProtoEvent{
					Id:        "ev-123",
					Timestamp: time.Now().UnixNano(),
					Metadata: map[string]string{
						v1.MetaType:     string(v1.ETSystem),
						v1.MetaEntityID: entityID,
					},
					Data: &v1.ProtoEvent_SystemData{
						SystemData: &v1.SystemData{
							Operator: string(v1.OpCreate),
							Data:     []byte(`{"id":"device123", "properties": {"temp": 20}}`),
						},
					},
				})
				fmt.Println("create entity: ", entityID)
			}
		}()
	}

	<-stopCh

}
