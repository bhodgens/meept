# ChatService

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Bus** | Pointer to **map[string]interface{}** |  | [optional] 
**AgentRegistry** | Pointer to **map[string]interface{}** |  | [optional] 
**SessionStore** | Pointer to **map[string]interface{}** |  | [optional] 
**Logger** | Pointer to **map[string]interface{}** |  | [optional] 

## Methods

### NewChatService

`func NewChatService() *ChatService`

NewChatService instantiates a new ChatService object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewChatServiceWithDefaults

`func NewChatServiceWithDefaults() *ChatService`

NewChatServiceWithDefaults instantiates a new ChatService object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetBus

`func (o *ChatService) GetBus() map[string]interface{}`

GetBus returns the Bus field if non-nil, zero value otherwise.

### GetBusOk

`func (o *ChatService) GetBusOk() (*map[string]interface{}, bool)`

GetBusOk returns a tuple with the Bus field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetBus

`func (o *ChatService) SetBus(v map[string]interface{})`

SetBus sets Bus field to given value.

### HasBus

`func (o *ChatService) HasBus() bool`

HasBus returns a boolean if a field has been set.

### SetBusNil

`func (o *ChatService) SetBusNil(b bool)`

 SetBusNil sets the value for Bus to be an explicit nil

### UnsetBus
`func (o *ChatService) UnsetBus()`

UnsetBus ensures that no value is present for Bus, not even an explicit nil
### GetAgentRegistry

`func (o *ChatService) GetAgentRegistry() map[string]interface{}`

GetAgentRegistry returns the AgentRegistry field if non-nil, zero value otherwise.

### GetAgentRegistryOk

`func (o *ChatService) GetAgentRegistryOk() (*map[string]interface{}, bool)`

GetAgentRegistryOk returns a tuple with the AgentRegistry field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetAgentRegistry

`func (o *ChatService) SetAgentRegistry(v map[string]interface{})`

SetAgentRegistry sets AgentRegistry field to given value.

### HasAgentRegistry

`func (o *ChatService) HasAgentRegistry() bool`

HasAgentRegistry returns a boolean if a field has been set.

### SetAgentRegistryNil

`func (o *ChatService) SetAgentRegistryNil(b bool)`

 SetAgentRegistryNil sets the value for AgentRegistry to be an explicit nil

### UnsetAgentRegistry
`func (o *ChatService) UnsetAgentRegistry()`

UnsetAgentRegistry ensures that no value is present for AgentRegistry, not even an explicit nil
### GetSessionStore

`func (o *ChatService) GetSessionStore() map[string]interface{}`

GetSessionStore returns the SessionStore field if non-nil, zero value otherwise.

### GetSessionStoreOk

`func (o *ChatService) GetSessionStoreOk() (*map[string]interface{}, bool)`

GetSessionStoreOk returns a tuple with the SessionStore field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSessionStore

`func (o *ChatService) SetSessionStore(v map[string]interface{})`

SetSessionStore sets SessionStore field to given value.

### HasSessionStore

`func (o *ChatService) HasSessionStore() bool`

HasSessionStore returns a boolean if a field has been set.

### GetLogger

`func (o *ChatService) GetLogger() map[string]interface{}`

GetLogger returns the Logger field if non-nil, zero value otherwise.

### GetLoggerOk

`func (o *ChatService) GetLoggerOk() (*map[string]interface{}, bool)`

GetLoggerOk returns a tuple with the Logger field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetLogger

`func (o *ChatService) SetLogger(v map[string]interface{})`

SetLogger sets Logger field to given value.

### HasLogger

`func (o *ChatService) HasLogger() bool`

HasLogger returns a boolean if a field has been set.

### SetLoggerNil

`func (o *ChatService) SetLoggerNil(b bool)`

 SetLoggerNil sets the value for Logger to be an explicit nil

### UnsetLogger
`func (o *ChatService) UnsetLogger()`

UnsetLogger ensures that no value is present for Logger, not even an explicit nil

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


