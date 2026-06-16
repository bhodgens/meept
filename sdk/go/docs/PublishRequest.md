# PublishRequest

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Topic** | **string** |  | 
**Type** | **string** |  | 
**Sourceomitempty** | Pointer to **string** |  | [optional] 
**Payloadomitempty** | Pointer to **NullableString** |  | [optional] 

## Methods

### NewPublishRequest

`func NewPublishRequest(topic string, type_ string, ) *PublishRequest`

NewPublishRequest instantiates a new PublishRequest object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewPublishRequestWithDefaults

`func NewPublishRequestWithDefaults() *PublishRequest`

NewPublishRequestWithDefaults instantiates a new PublishRequest object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetTopic

`func (o *PublishRequest) GetTopic() string`

GetTopic returns the Topic field if non-nil, zero value otherwise.

### GetTopicOk

`func (o *PublishRequest) GetTopicOk() (*string, bool)`

GetTopicOk returns a tuple with the Topic field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetTopic

`func (o *PublishRequest) SetTopic(v string)`

SetTopic sets Topic field to given value.


### GetType

`func (o *PublishRequest) GetType() string`

GetType returns the Type field if non-nil, zero value otherwise.

### GetTypeOk

`func (o *PublishRequest) GetTypeOk() (*string, bool)`

GetTypeOk returns a tuple with the Type field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetType

`func (o *PublishRequest) SetType(v string)`

SetType sets Type field to given value.


### GetSourceomitempty

`func (o *PublishRequest) GetSourceomitempty() string`

GetSourceomitempty returns the Sourceomitempty field if non-nil, zero value otherwise.

### GetSourceomitemptyOk

`func (o *PublishRequest) GetSourceomitemptyOk() (*string, bool)`

GetSourceomitemptyOk returns a tuple with the Sourceomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSourceomitempty

`func (o *PublishRequest) SetSourceomitempty(v string)`

SetSourceomitempty sets Sourceomitempty field to given value.

### HasSourceomitempty

`func (o *PublishRequest) HasSourceomitempty() bool`

HasSourceomitempty returns a boolean if a field has been set.

### GetPayloadomitempty

`func (o *PublishRequest) GetPayloadomitempty() string`

GetPayloadomitempty returns the Payloadomitempty field if non-nil, zero value otherwise.

### GetPayloadomitemptyOk

`func (o *PublishRequest) GetPayloadomitemptyOk() (*string, bool)`

GetPayloadomitemptyOk returns a tuple with the Payloadomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPayloadomitempty

`func (o *PublishRequest) SetPayloadomitempty(v string)`

SetPayloadomitempty sets Payloadomitempty field to given value.

### HasPayloadomitempty

`func (o *PublishRequest) HasPayloadomitempty() bool`

HasPayloadomitempty returns a boolean if a field has been set.

### SetPayloadomitemptyNil

`func (o *PublishRequest) SetPayloadomitemptyNil(b bool)`

 SetPayloadomitemptyNil sets the value for Payloadomitempty to be an explicit nil

### UnsetPayloadomitempty
`func (o *PublishRequest) UnsetPayloadomitempty()`

UnsetPayloadomitempty ensures that no value is present for Payloadomitempty, not even an explicit nil

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


