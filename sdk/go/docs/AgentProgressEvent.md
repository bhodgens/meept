# AgentProgressEvent

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Type** | Pointer to **string** |  | [optional] 
**SessionId** | Pointer to **string** |  | [optional] 
**AgentId** | Pointer to **string** |  | [optional] 
**Message** | Pointer to **string** |  | [optional] 
**Tier** | Pointer to **int32** |  | [optional] 
**SourceEvent** | Pointer to **string** |  | [optional] 
**Timestamp** | Pointer to **time.Time** |  | [optional] 

## Methods

### NewAgentProgressEvent

`func NewAgentProgressEvent() *AgentProgressEvent`

NewAgentProgressEvent instantiates a new AgentProgressEvent object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewAgentProgressEventWithDefaults

`func NewAgentProgressEventWithDefaults() *AgentProgressEvent`

NewAgentProgressEventWithDefaults instantiates a new AgentProgressEvent object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetType

`func (o *AgentProgressEvent) GetType() string`

GetType returns the Type field if non-nil, zero value otherwise.

### GetTypeOk

`func (o *AgentProgressEvent) GetTypeOk() (*string, bool)`

GetTypeOk returns a tuple with the Type field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetType

`func (o *AgentProgressEvent) SetType(v string)`

SetType sets Type field to given value.

### HasType

`func (o *AgentProgressEvent) HasType() bool`

HasType returns a boolean if a field has been set.

### GetSessionId

`func (o *AgentProgressEvent) GetSessionId() string`

GetSessionId returns the SessionId field if non-nil, zero value otherwise.

### GetSessionIdOk

`func (o *AgentProgressEvent) GetSessionIdOk() (*string, bool)`

GetSessionIdOk returns a tuple with the SessionId field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSessionId

`func (o *AgentProgressEvent) SetSessionId(v string)`

SetSessionId sets SessionId field to given value.

### HasSessionId

`func (o *AgentProgressEvent) HasSessionId() bool`

HasSessionId returns a boolean if a field has been set.

### GetAgentId

`func (o *AgentProgressEvent) GetAgentId() string`

GetAgentId returns the AgentId field if non-nil, zero value otherwise.

### GetAgentIdOk

`func (o *AgentProgressEvent) GetAgentIdOk() (*string, bool)`

GetAgentIdOk returns a tuple with the AgentId field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetAgentId

`func (o *AgentProgressEvent) SetAgentId(v string)`

SetAgentId sets AgentId field to given value.

### HasAgentId

`func (o *AgentProgressEvent) HasAgentId() bool`

HasAgentId returns a boolean if a field has been set.

### GetMessage

`func (o *AgentProgressEvent) GetMessage() string`

GetMessage returns the Message field if non-nil, zero value otherwise.

### GetMessageOk

`func (o *AgentProgressEvent) GetMessageOk() (*string, bool)`

GetMessageOk returns a tuple with the Message field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetMessage

`func (o *AgentProgressEvent) SetMessage(v string)`

SetMessage sets Message field to given value.

### HasMessage

`func (o *AgentProgressEvent) HasMessage() bool`

HasMessage returns a boolean if a field has been set.

### GetTier

`func (o *AgentProgressEvent) GetTier() int32`

GetTier returns the Tier field if non-nil, zero value otherwise.

### GetTierOk

`func (o *AgentProgressEvent) GetTierOk() (*int32, bool)`

GetTierOk returns a tuple with the Tier field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetTier

`func (o *AgentProgressEvent) SetTier(v int32)`

SetTier sets Tier field to given value.

### HasTier

`func (o *AgentProgressEvent) HasTier() bool`

HasTier returns a boolean if a field has been set.

### GetSourceEvent

`func (o *AgentProgressEvent) GetSourceEvent() string`

GetSourceEvent returns the SourceEvent field if non-nil, zero value otherwise.

### GetSourceEventOk

`func (o *AgentProgressEvent) GetSourceEventOk() (*string, bool)`

GetSourceEventOk returns a tuple with the SourceEvent field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSourceEvent

`func (o *AgentProgressEvent) SetSourceEvent(v string)`

SetSourceEvent sets SourceEvent field to given value.

### HasSourceEvent

`func (o *AgentProgressEvent) HasSourceEvent() bool`

HasSourceEvent returns a boolean if a field has been set.

### GetTimestamp

`func (o *AgentProgressEvent) GetTimestamp() time.Time`

GetTimestamp returns the Timestamp field if non-nil, zero value otherwise.

### GetTimestampOk

`func (o *AgentProgressEvent) GetTimestampOk() (*time.Time, bool)`

GetTimestampOk returns a tuple with the Timestamp field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetTimestamp

`func (o *AgentProgressEvent) SetTimestamp(v time.Time)`

SetTimestamp sets Timestamp field to given value.

### HasTimestamp

`func (o *AgentProgressEvent) HasTimestamp() bool`

HasTimestamp returns a boolean if a field has been set.


[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


