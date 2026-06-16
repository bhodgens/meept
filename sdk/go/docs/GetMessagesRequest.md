# GetMessagesRequest

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Id** | **string** |  | 
**Offsetomitempty** | Pointer to **int32** |  | [optional] 
**Limitomitempty** | Pointer to **int32** |  | [optional] 

## Methods

### NewGetMessagesRequest

`func NewGetMessagesRequest(id string, ) *GetMessagesRequest`

NewGetMessagesRequest instantiates a new GetMessagesRequest object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewGetMessagesRequestWithDefaults

`func NewGetMessagesRequestWithDefaults() *GetMessagesRequest`

NewGetMessagesRequestWithDefaults instantiates a new GetMessagesRequest object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetId

`func (o *GetMessagesRequest) GetId() string`

GetId returns the Id field if non-nil, zero value otherwise.

### GetIdOk

`func (o *GetMessagesRequest) GetIdOk() (*string, bool)`

GetIdOk returns a tuple with the Id field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetId

`func (o *GetMessagesRequest) SetId(v string)`

SetId sets Id field to given value.


### GetOffsetomitempty

`func (o *GetMessagesRequest) GetOffsetomitempty() int32`

GetOffsetomitempty returns the Offsetomitempty field if non-nil, zero value otherwise.

### GetOffsetomitemptyOk

`func (o *GetMessagesRequest) GetOffsetomitemptyOk() (*int32, bool)`

GetOffsetomitemptyOk returns a tuple with the Offsetomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetOffsetomitempty

`func (o *GetMessagesRequest) SetOffsetomitempty(v int32)`

SetOffsetomitempty sets Offsetomitempty field to given value.

### HasOffsetomitempty

`func (o *GetMessagesRequest) HasOffsetomitempty() bool`

HasOffsetomitempty returns a boolean if a field has been set.

### GetLimitomitempty

`func (o *GetMessagesRequest) GetLimitomitempty() int32`

GetLimitomitempty returns the Limitomitempty field if non-nil, zero value otherwise.

### GetLimitomitemptyOk

`func (o *GetMessagesRequest) GetLimitomitemptyOk() (*int32, bool)`

GetLimitomitemptyOk returns a tuple with the Limitomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetLimitomitempty

`func (o *GetMessagesRequest) SetLimitomitempty(v int32)`

SetLimitomitempty sets Limitomitempty field to given value.

### HasLimitomitempty

`func (o *GetMessagesRequest) HasLimitomitempty() bool`

HasLimitomitempty returns a boolean if a field has been set.


[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


