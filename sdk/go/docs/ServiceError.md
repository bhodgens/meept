# ServiceError

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Service** | Pointer to **string** |  | [optional] 
**Op** | Pointer to **string** |  | [optional] 
**Err** | Pointer to **map[string]interface{}** |  | [optional] 

## Methods

### NewServiceError

`func NewServiceError() *ServiceError`

NewServiceError instantiates a new ServiceError object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewServiceErrorWithDefaults

`func NewServiceErrorWithDefaults() *ServiceError`

NewServiceErrorWithDefaults instantiates a new ServiceError object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetService

`func (o *ServiceError) GetService() string`

GetService returns the Service field if non-nil, zero value otherwise.

### GetServiceOk

`func (o *ServiceError) GetServiceOk() (*string, bool)`

GetServiceOk returns a tuple with the Service field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetService

`func (o *ServiceError) SetService(v string)`

SetService sets Service field to given value.

### HasService

`func (o *ServiceError) HasService() bool`

HasService returns a boolean if a field has been set.

### GetOp

`func (o *ServiceError) GetOp() string`

GetOp returns the Op field if non-nil, zero value otherwise.

### GetOpOk

`func (o *ServiceError) GetOpOk() (*string, bool)`

GetOpOk returns a tuple with the Op field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetOp

`func (o *ServiceError) SetOp(v string)`

SetOp sets Op field to given value.

### HasOp

`func (o *ServiceError) HasOp() bool`

HasOp returns a boolean if a field has been set.

### GetErr

`func (o *ServiceError) GetErr() map[string]interface{}`

GetErr returns the Err field if non-nil, zero value otherwise.

### GetErrOk

`func (o *ServiceError) GetErrOk() (*map[string]interface{}, bool)`

GetErrOk returns a tuple with the Err field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetErr

`func (o *ServiceError) SetErr(v map[string]interface{})`

SetErr sets Err field to given value.

### HasErr

`func (o *ServiceError) HasErr() bool`

HasErr returns a boolean if a field has been set.


[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


