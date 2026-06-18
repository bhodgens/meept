//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'audit_entry.g.dart';

/// AuditEntry
///
/// Properties:
/// * [timestamp] 
/// * [action] 
/// * [resource] 
/// * [allowed] 
@BuiltValue()
abstract class AuditEntry implements Built<AuditEntry, AuditEntryBuilder> {
  @BuiltValueField(wireName: r'timestamp')
  String get timestamp;

  @BuiltValueField(wireName: r'action')
  String get action;

  @BuiltValueField(wireName: r'resource')
  String get resource;

  @BuiltValueField(wireName: r'allowed')
  bool get allowed;

  AuditEntry._();

  factory AuditEntry([void updates(AuditEntryBuilder b)]) = _$AuditEntry;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(AuditEntryBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<AuditEntry> get serializer => _$AuditEntrySerializer();
}

class _$AuditEntrySerializer implements PrimitiveSerializer<AuditEntry> {
  @override
  final Iterable<Type> types = const [AuditEntry, _$AuditEntry];

  @override
  final String wireName = r'AuditEntry';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    AuditEntry object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'timestamp';
    yield serializers.serialize(
      object.timestamp,
      specifiedType: const FullType(String),
    );
    yield r'action';
    yield serializers.serialize(
      object.action,
      specifiedType: const FullType(String),
    );
    yield r'resource';
    yield serializers.serialize(
      object.resource,
      specifiedType: const FullType(String),
    );
    yield r'allowed';
    yield serializers.serialize(
      object.allowed,
      specifiedType: const FullType(bool),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    AuditEntry object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required AuditEntryBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'timestamp':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.timestamp = valueDes;
          break;
        case r'action':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.action = valueDes;
          break;
        case r'resource':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.resource = valueDes;
          break;
        case r'allowed':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(bool),
          ) as bool;
          result.allowed = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  AuditEntry deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = AuditEntryBuilder();
    final serializedList = (serialized as Iterable<Object?>).toList();
    final unhandled = <Object?>[];
    _deserializeProperties(
      serializers,
      serialized,
      specifiedType: specifiedType,
      serializedList: serializedList,
      unhandled: unhandled,
      result: result,
    );
    return result.build();
  }
}

