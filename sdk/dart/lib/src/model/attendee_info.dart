//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'attendee_info.g.dart';

/// AttendeeInfo
///
/// Properties:
/// * [email] 
/// * [displayName] 
/// * [response] 
@BuiltValue()
abstract class AttendeeInfo implements Built<AttendeeInfo, AttendeeInfoBuilder> {
  @BuiltValueField(wireName: r'email')
  String get email;

  @BuiltValueField(wireName: r'display_name')
  String? get displayName;

  @BuiltValueField(wireName: r'response')
  String? get response;

  AttendeeInfo._();

  factory AttendeeInfo([void updates(AttendeeInfoBuilder b)]) = _$AttendeeInfo;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(AttendeeInfoBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<AttendeeInfo> get serializer => _$AttendeeInfoSerializer();
}

class _$AttendeeInfoSerializer implements PrimitiveSerializer<AttendeeInfo> {
  @override
  final Iterable<Type> types = const [AttendeeInfo, _$AttendeeInfo];

  @override
  final String wireName = r'AttendeeInfo';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    AttendeeInfo object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'email';
    yield serializers.serialize(
      object.email,
      specifiedType: const FullType(String),
    );
    if (object.displayName != null) {
      yield r'display_name';
      yield serializers.serialize(
        object.displayName,
        specifiedType: const FullType(String),
      );
    }
    if (object.response != null) {
      yield r'response';
      yield serializers.serialize(
        object.response,
        specifiedType: const FullType(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    AttendeeInfo object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required AttendeeInfoBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'email':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.email = valueDes;
          break;
        case r'display_name':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.displayName = valueDes;
          break;
        case r'response':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.response = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  AttendeeInfo deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = AttendeeInfoBuilder();
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

