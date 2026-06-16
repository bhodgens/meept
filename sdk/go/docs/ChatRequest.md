# ChatRequest

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Message** | **string** |  | 
**ConversationId** | **string** |  | 
**AgentIdomitempty** | Pointer to **string** |  | [optional] 

## Methods

### NewChatRequest

`func NewChatRequest(message string, conversationId string, ) *ChatRequest`

NewChatRequest instantiates a new ChatRequest object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewChatRequestWithDefaults

`func NewChatRequestWithDefaults() *ChatRequest`

NewChatRequestWithDefaults instantiates a new ChatRequest object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetMessage

`func (o *ChatRequest) GetMessage() string`

GetMessage returns the Message field if non-nil, zero value otherwise.

### GetMessageOk

`func (o *ChatRequest) GetMessageOk() (*string, bool)`

GetMessageOk returns a tuple with the Message field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetMessage

`func (o *ChatRequest) SetMessage(v string)`

SetMessage sets Message field to given value.


### GetConversationId

`func (o *ChatRequest) GetConversationId() string`

GetConversationId returns the ConversationId field if non-nil, zero value otherwise.

### GetConversationIdOk

`func (o *ChatRequest) GetConversationIdOk() (*string, bool)`

GetConversationIdOk returns a tuple with the ConversationId field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetConversationId

`func (o *ChatRequest) SetConversationId(v string)`

SetConversationId sets ConversationId field to given value.


### GetAgentIdomitempty

`func (o *ChatRequest) GetAgentIdomitempty() string`

GetAgentIdomitempty returns the AgentIdomitempty field if non-nil, zero value otherwise.

### GetAgentIdomitemptyOk

`func (o *ChatRequest) GetAgentIdomitemptyOk() (*string, bool)`

GetAgentIdomitemptyOk returns a tuple with the AgentIdomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetAgentIdomitempty

`func (o *ChatRequest) SetAgentIdomitempty(v string)`

SetAgentIdomitempty sets AgentIdomitempty field to given value.

### HasAgentIdomitempty

`func (o *ChatRequest) HasAgentIdomitempty() bool`

HasAgentIdomitempty returns a boolean if a field has been set.


[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


