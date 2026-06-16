# AuditEntry

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Timestamp** | **string** |  | 
**Action** | **string** |  | 
**Resource** | **string** |  | 
**Allowed** | **bool** |  | 

## Methods

### NewAuditEntry

`func NewAuditEntry(timestamp string, action string, resource string, allowed bool, ) *AuditEntry`

NewAuditEntry instantiates a new AuditEntry object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewAuditEntryWithDefaults

`func NewAuditEntryWithDefaults() *AuditEntry`

NewAuditEntryWithDefaults instantiates a new AuditEntry object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetTimestamp

`func (o *AuditEntry) GetTimestamp() string`

GetTimestamp returns the Timestamp field if non-nil, zero value otherwise.

### GetTimestampOk

`func (o *AuditEntry) GetTimestampOk() (*string, bool)`

GetTimestampOk returns a tuple with the Timestamp field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetTimestamp

`func (o *AuditEntry) SetTimestamp(v string)`

SetTimestamp sets Timestamp field to given value.


### GetAction

`func (o *AuditEntry) GetAction() string`

GetAction returns the Action field if non-nil, zero value otherwise.

### GetActionOk

`func (o *AuditEntry) GetActionOk() (*string, bool)`

GetActionOk returns a tuple with the Action field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetAction

`func (o *AuditEntry) SetAction(v string)`

SetAction sets Action field to given value.


### GetResource

`func (o *AuditEntry) GetResource() string`

GetResource returns the Resource field if non-nil, zero value otherwise.

### GetResourceOk

`func (o *AuditEntry) GetResourceOk() (*string, bool)`

GetResourceOk returns a tuple with the Resource field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetResource

`func (o *AuditEntry) SetResource(v string)`

SetResource sets Resource field to given value.


### GetAllowed

`func (o *AuditEntry) GetAllowed() bool`

GetAllowed returns the Allowed field if non-nil, zero value otherwise.

### GetAllowedOk

`func (o *AuditEntry) GetAllowedOk() (*bool, bool)`

GetAllowedOk returns a tuple with the Allowed field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetAllowed

`func (o *AuditEntry) SetAllowed(v bool)`

SetAllowed sets Allowed field to given value.



[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


