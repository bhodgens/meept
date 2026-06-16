# ChatResponse

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Reply** | **string** |  | 
**Modelomitempty** | Pointer to **string** |  | [optional] 
**TokensUsedomitempty** | Pointer to **int32** |  | [optional] 

## Methods

### NewChatResponse

`func NewChatResponse(reply string, ) *ChatResponse`

NewChatResponse instantiates a new ChatResponse object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewChatResponseWithDefaults

`func NewChatResponseWithDefaults() *ChatResponse`

NewChatResponseWithDefaults instantiates a new ChatResponse object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetReply

`func (o *ChatResponse) GetReply() string`

GetReply returns the Reply field if non-nil, zero value otherwise.

### GetReplyOk

`func (o *ChatResponse) GetReplyOk() (*string, bool)`

GetReplyOk returns a tuple with the Reply field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetReply

`func (o *ChatResponse) SetReply(v string)`

SetReply sets Reply field to given value.


### GetModelomitempty

`func (o *ChatResponse) GetModelomitempty() string`

GetModelomitempty returns the Modelomitempty field if non-nil, zero value otherwise.

### GetModelomitemptyOk

`func (o *ChatResponse) GetModelomitemptyOk() (*string, bool)`

GetModelomitemptyOk returns a tuple with the Modelomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetModelomitempty

`func (o *ChatResponse) SetModelomitempty(v string)`

SetModelomitempty sets Modelomitempty field to given value.

### HasModelomitempty

`func (o *ChatResponse) HasModelomitempty() bool`

HasModelomitempty returns a boolean if a field has been set.

### GetTokensUsedomitempty

`func (o *ChatResponse) GetTokensUsedomitempty() int32`

GetTokensUsedomitempty returns the TokensUsedomitempty field if non-nil, zero value otherwise.

### GetTokensUsedomitemptyOk

`func (o *ChatResponse) GetTokensUsedomitemptyOk() (*int32, bool)`

GetTokensUsedomitemptyOk returns a tuple with the TokensUsedomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetTokensUsedomitempty

`func (o *ChatResponse) SetTokensUsedomitempty(v int32)`

SetTokensUsedomitempty sets TokensUsedomitempty field to given value.

### HasTokensUsedomitempty

`func (o *ChatResponse) HasTokensUsedomitempty() bool`

HasTokensUsedomitempty returns a boolean if a field has been set.


[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


