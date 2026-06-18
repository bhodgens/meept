//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'provider_info.g.dart';

/// ProviderInfo
///
/// Properties:
/// * [id] 
/// * [name] 
/// * [api] 
/// * [baseUrl] 
/// * [models] 
/// * [hasCredentials] 
@BuiltValue()
abstract class ProviderInfo implements Built<ProviderInfo, ProviderInfoBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  @BuiltValueField(wireName: r'name')
  String get name;

  @BuiltValueField(wireName: r'api')
  String get api;

  @BuiltValueField(wireName: r'base_url')
  String get baseUrl;

  @BuiltValueField(wireName: r'models')
  String? get models;

  @BuiltValueField(wireName: r'has_credentials')
  bool get hasCredentials;

  ProviderInfo._();

  factory ProviderInfo([void updates(ProviderInfoBuilder b)]) = _$ProviderInfo;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ProviderInfoBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ProviderInfo> get serializer => _$ProviderInfoSerializer();
}

class _$ProviderInfoSerializer implements PrimitiveSerializer<ProviderInfo> {
  @override
  final Iterable<Type> types = const [ProviderInfo, _$ProviderInfo];

  @override
  final String wireName = r'ProviderInfo';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ProviderInfo object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'id';
    yield serializers.serialize(
      object.id,
      specifiedType: const FullType(String),
    );
    yield r'name';
    yield serializers.serialize(
      object.name,
      specifiedType: const FullType(String),
    );
    yield r'api';
    yield serializers.serialize(
      object.api,
      specifiedType: const FullType(String),
    );
    yield r'base_url';
    yield serializers.serialize(
      object.baseUrl,
      specifiedType: const FullType(String),
    );
    yield r'models';
    yield object.models == null ? null : serializers.serialize(
      object.models,
      specifiedType: const FullType.nullable(String),
    );
    yield r'has_credentials';
    yield serializers.serialize(
      object.hasCredentials,
      specifiedType: const FullType(bool),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    ProviderInfo object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ProviderInfoBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.id = valueDes;
          break;
        case r'name':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.name = valueDes;
          break;
        case r'api':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.api = valueDes;
          break;
        case r'base_url':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.baseUrl = valueDes;
          break;
        case r'models':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.models = valueDes;
          break;
        case r'has_credentials':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(bool),
          ) as bool;
          result.hasCredentials = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  ProviderInfo deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ProviderInfoBuilder();
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

