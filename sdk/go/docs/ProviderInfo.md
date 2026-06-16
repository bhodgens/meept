# ProviderInfo

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Id** | **string** |  | 
**Name** | **string** |  | 
**Api** | **string** |  | 
**BaseUrl** | **string** |  | 
**Models** | **NullableString** |  | 
**HasCredentials** | **bool** |  | 

## Methods

### NewProviderInfo

`func NewProviderInfo(id string, name string, api string, baseUrl string, models NullableString, hasCredentials bool, ) *ProviderInfo`

NewProviderInfo instantiates a new ProviderInfo object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewProviderInfoWithDefaults

`func NewProviderInfoWithDefaults() *ProviderInfo`

NewProviderInfoWithDefaults instantiates a new ProviderInfo object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetId

`func (o *ProviderInfo) GetId() string`

GetId returns the Id field if non-nil, zero value otherwise.

### GetIdOk

`func (o *ProviderInfo) GetIdOk() (*string, bool)`

GetIdOk returns a tuple with the Id field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetId

`func (o *ProviderInfo) SetId(v string)`

SetId sets Id field to given value.


### GetName

`func (o *ProviderInfo) GetName() string`

GetName returns the Name field if non-nil, zero value otherwise.

### GetNameOk

`func (o *ProviderInfo) GetNameOk() (*string, bool)`

GetNameOk returns a tuple with the Name field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetName

`func (o *ProviderInfo) SetName(v string)`

SetName sets Name field to given value.


### GetApi

`func (o *ProviderInfo) GetApi() string`

GetApi returns the Api field if non-nil, zero value otherwise.

### GetApiOk

`func (o *ProviderInfo) GetApiOk() (*string, bool)`

GetApiOk returns a tuple with the Api field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetApi

`func (o *ProviderInfo) SetApi(v string)`

SetApi sets Api field to given value.


### GetBaseUrl

`func (o *ProviderInfo) GetBaseUrl() string`

GetBaseUrl returns the BaseUrl field if non-nil, zero value otherwise.

### GetBaseUrlOk

`func (o *ProviderInfo) GetBaseUrlOk() (*string, bool)`

GetBaseUrlOk returns a tuple with the BaseUrl field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetBaseUrl

`func (o *ProviderInfo) SetBaseUrl(v string)`

SetBaseUrl sets BaseUrl field to given value.


### GetModels

`func (o *ProviderInfo) GetModels() string`

GetModels returns the Models field if non-nil, zero value otherwise.

### GetModelsOk

`func (o *ProviderInfo) GetModelsOk() (*string, bool)`

GetModelsOk returns a tuple with the Models field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetModels

`func (o *ProviderInfo) SetModels(v string)`

SetModels sets Models field to given value.


### SetModelsNil

`func (o *ProviderInfo) SetModelsNil(b bool)`

 SetModelsNil sets the value for Models to be an explicit nil

### UnsetModels
`func (o *ProviderInfo) UnsetModels()`

UnsetModels ensures that no value is present for Models, not even an explicit nil
### GetHasCredentials

`func (o *ProviderInfo) GetHasCredentials() bool`

GetHasCredentials returns the HasCredentials field if non-nil, zero value otherwise.

### GetHasCredentialsOk

`func (o *ProviderInfo) GetHasCredentialsOk() (*bool, bool)`

GetHasCredentialsOk returns a tuple with the HasCredentials field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetHasCredentials

`func (o *ProviderInfo) SetHasCredentials(v bool)`

SetHasCredentials sets HasCredentials field to given value.



[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


