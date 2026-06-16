# WSSubscribeMessage

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Type** | Pointer to **string** |  | [optional] 
**Channel** | Pointer to **string** |  | [optional] 
**SessionId** | Pointer to **string** |  | [optional] 

## Methods

### NewWSSubscribeMessage

`func NewWSSubscribeMessage() *WSSubscribeMessage`

NewWSSubscribeMessage instantiates a new WSSubscribeMessage object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewWSSubscribeMessageWithDefaults

`func NewWSSubscribeMessageWithDefaults() *WSSubscribeMessage`

NewWSSubscribeMessageWithDefaults instantiates a new WSSubscribeMessage object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetType

`func (o *WSSubscribeMessage) GetType() string`

GetType returns the Type field if non-nil, zero value otherwise.

### GetTypeOk

`func (o *WSSubscribeMessage) GetTypeOk() (*string, bool)`

GetTypeOk returns a tuple with the Type field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetType

`func (o *WSSubscribeMessage) SetType(v string)`

SetType sets Type field to given value.

### HasType

`func (o *WSSubscribeMessage) HasType() bool`

HasType returns a boolean if a field has been set.

### GetChannel

`func (o *WSSubscribeMessage) GetChannel() string`

GetChannel returns the Channel field if non-nil, zero value otherwise.

### GetChannelOk

`func (o *WSSubscribeMessage) GetChannelOk() (*string, bool)`

GetChannelOk returns a tuple with the Channel field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetChannel

`func (o *WSSubscribeMessage) SetChannel(v string)`

SetChannel sets Channel field to given value.

### HasChannel

`func (o *WSSubscribeMessage) HasChannel() bool`

HasChannel returns a boolean if a field has been set.

### GetSessionId

`func (o *WSSubscribeMessage) GetSessionId() string`

GetSessionId returns the SessionId field if non-nil, zero value otherwise.

### GetSessionIdOk

`func (o *WSSubscribeMessage) GetSessionIdOk() (*string, bool)`

GetSessionIdOk returns a tuple with the SessionId field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSessionId

`func (o *WSSubscribeMessage) SetSessionId(v string)`

SetSessionId sets SessionId field to given value.

### HasSessionId

`func (o *WSSubscribeMessage) HasSessionId() bool`

HasSessionId returns a boolean if a field has been set.


[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


