# ForkSessionRequest

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**SessionId** | **string** |  | 
**FromMessageId** | **int32** |  | 
**Nameomitempty** | Pointer to **string** |  | [optional] 

## Methods

### NewForkSessionRequest

`func NewForkSessionRequest(sessionId string, fromMessageId int32, ) *ForkSessionRequest`

NewForkSessionRequest instantiates a new ForkSessionRequest object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewForkSessionRequestWithDefaults

`func NewForkSessionRequestWithDefaults() *ForkSessionRequest`

NewForkSessionRequestWithDefaults instantiates a new ForkSessionRequest object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetSessionId

`func (o *ForkSessionRequest) GetSessionId() string`

GetSessionId returns the SessionId field if non-nil, zero value otherwise.

### GetSessionIdOk

`func (o *ForkSessionRequest) GetSessionIdOk() (*string, bool)`

GetSessionIdOk returns a tuple with the SessionId field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSessionId

`func (o *ForkSessionRequest) SetSessionId(v string)`

SetSessionId sets SessionId field to given value.


### GetFromMessageId

`func (o *ForkSessionRequest) GetFromMessageId() int32`

GetFromMessageId returns the FromMessageId field if non-nil, zero value otherwise.

### GetFromMessageIdOk

`func (o *ForkSessionRequest) GetFromMessageIdOk() (*int32, bool)`

GetFromMessageIdOk returns a tuple with the FromMessageId field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetFromMessageId

`func (o *ForkSessionRequest) SetFromMessageId(v int32)`

SetFromMessageId sets FromMessageId field to given value.


### GetNameomitempty

`func (o *ForkSessionRequest) GetNameomitempty() string`

GetNameomitempty returns the Nameomitempty field if non-nil, zero value otherwise.

### GetNameomitemptyOk

`func (o *ForkSessionRequest) GetNameomitemptyOk() (*string, bool)`

GetNameomitemptyOk returns a tuple with the Nameomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetNameomitempty

`func (o *ForkSessionRequest) SetNameomitempty(v string)`

SetNameomitempty sets Nameomitempty field to given value.

### HasNameomitempty

`func (o *ForkSessionRequest) HasNameomitempty() bool`

HasNameomitempty returns a boolean if a field has been set.


[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


