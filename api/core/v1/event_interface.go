// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.27.1
// 	protoc        v3.19.1
// source: api/core/v1/event.proto

package v1

import (
	"fmt"

	"github.com/golang/protobuf/proto"
)

// definition message header field.
const (
	MetaTTL             = "x-msg-ttl"
	MetaType            = "x-msg-type"
	MetaTopic           = "x-msg-topic"
	MetaEntityID        = "x-msg-en-id"
	MetaEntityType      = "x-msg-en-type"
	MetaOwner           = "x-msg-owner"
	MetaSource          = "x-msg-source"
	MetaVersion         = "x-msg-version"
	MetaSender          = "x-msg-sender"
	MetaRequestID       = "x-msg-request-id"
	MetaResponseStatus  = "x-msg-response-status"
	MetaResponseErrCode = "x-msg-response-errcode"
	MetaPathConstructor = "x-msg-path-constructor"
)

type PathConstructor string 

const (
	PCDefault PathConstructor = "default"
	PCScheme PathConstructor = "scheme"
)



type EventType string

const (
	ETCache    EventType = "core.event.Cache"
	ETEntity   EventType = "core.event.Entity"
	ETSystem   EventType = "core.event.System"
	ETCallback EventType = "core.event.Callback"
)

type SystemOp string

const (
	OpCreate SystemOp = "core.event.System.Create"
	OpDelete SystemOp = "core.event.System.Delete"
)

type Attribution interface {
	Attr(key string) string
	SetAttr(key string, value string) Event
	ForeachAttr(handler func(key, val string))
}

type Event interface {
	Attribution

	ID() string
	Copy() Event
	Type() EventType
	SetType(t EventType)
	Version() string
	Validate() error
	Entity() string
	SetEntity(entityID string) Event
	SetTTL(td int) Event
	Attributes() map[string]string

	RawData() []byte
	Payload() isProtoEvent_Data
	SetPayload(payload isProtoEvent_Data) Event
	CallbackAddr() string
}

func (e *ProtoEvent) ID() string {
	return e.Id
}

func (e *ProtoEvent) Copy() Event {
	return e
}

func (e *ProtoEvent) Type() EventType {
	return EventType(e.Metadata[MetaType])
}

func (e *ProtoEvent) SetType(t EventType) {
	e.Metadata[MetaType] = string(t)
}

func (e *ProtoEvent) Version() string {
	return e.Metadata[MetaVersion]
}

func (e *ProtoEvent) Validate() error {
	return nil
}

func (e *ProtoEvent) Entity() string {
	return e.Metadata[MetaEntityID]
}

func (e *ProtoEvent) SetEntity(entityID string) Event {
	e.Metadata[MetaEntityID] = entityID
	return e
}

func (e *ProtoEvent) SetTTL(ttl int) Event {
	e.Metadata[MetaTTL] = fmt.Sprintf("%d", ttl)
	return e
}

func (e *ProtoEvent) RawData() []byte {
	return e.GetRawData()
}

func (e *ProtoEvent) Payload() isProtoEvent_Data { //nolint
	return e.GetData()
}

func (e *ProtoEvent) SetPayload(payload isProtoEvent_Data) Event { //nolint
	e.Data = payload
	return e
}

func (e *ProtoEvent) Attr(key string) string {
	return e.Metadata[key]
}

func (e *ProtoEvent) SetAttr(key string, value string) Event {
	e.Metadata[key] = value
	return e
}

func (e *ProtoEvent) ForeachAttr(handler func(key, val string)) {
	for key, val := range e.Metadata {
		handler(key, val)
	}
}

func (e *ProtoEvent) CallbackAddr() string {
	return e.Callback
}

func (e *ProtoEvent) Attributes() map[string]string {
	// copy ?.
	return e.Metadata
}

// ----------------------

type PatchEvent interface {
	Event
	Patches() []*PatchData
}

func (e *ProtoEvent) Patches() []*PatchData {
	switch data := e.Data.(type) {
	case *ProtoEvent_RawData:
		return []*PatchData{}
	case *ProtoEvent_Patches:
		return data.Patches.Patches
	}
	panic("invalid data type")
}

func Marshal(e Event) ([]byte, error) {
	ev, _ := e.(*ProtoEvent)
	return proto.Marshal(ev)
}

func Unmarshal(data []byte, e *ProtoEvent) error {
	return proto.Unmarshal(data, e)
}

//-----------------------------------------------

type SystemEvent interface {
	Event
	Action() *SystemData
}

func (e *ProtoEvent) Action() *SystemData {
	return e.GetSystemData()
}
