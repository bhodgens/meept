# AddWorkerRequest

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Id** | **string** |  | 
**Capabilities** | **NullableString** |  | 

## Methods

### NewAddWorkerRequest

`func NewAddWorkerRequest(id string, capabilities NullableString, ) *AddWorkerRequest`

NewAddWorkerRequest instantiates a new AddWorkerRequest object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewAddWorkerRequestWithDefaults

`func NewAddWorkerRequestWithDefaults() *AddWorkerRequest`

NewAddWorkerRequestWithDefaults instantiates a new AddWorkerRequest object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetId

`func (o *AddWorkerRequest) GetId() string`

GetId returns the Id field if non-nil, zero value otherwise.

### GetIdOk

`func (o *AddWorkerRequest) GetIdOk() (*string, bool)`

GetIdOk returns a tuple with the Id field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetId

`func (o *AddWorkerRequest) SetId(v string)`

SetId sets Id field to given value.


### GetCapabilities

`func (o *AddWorkerRequest) GetCapabilities() string`

GetCapabilities returns the Capabilities field if non-nil, zero value otherwise.

### GetCapabilitiesOk

`func (o *AddWorkerRequest) GetCapabilitiesOk() (*string, bool)`

GetCapabilitiesOk returns a tuple with the Capabilities field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetCapabilities

`func (o *AddWorkerRequest) SetCapabilities(v string)`

SetCapabilities sets Capabilities field to given value.


### SetCapabilitiesNil

`func (o *AddWorkerRequest) SetCapabilitiesNil(b bool)`

 SetCapabilitiesNil sets the value for Capabilities to be an explicit nil

### UnsetCapabilities
`func (o *AddWorkerRequest) UnsetCapabilities()`

UnsetCapabilities ensures that no value is present for Capabilities, not even an explicit nil

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


