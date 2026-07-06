//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'register_project_request.g.dart';

/// RegisterProjectRequest
///
/// Properties:
/// * [id] 
/// * [name] 
/// * [gitUrl] 
/// * [localPath] 
@BuiltValue()
abstract class RegisterProjectRequest implements Built<RegisterProjectRequest, RegisterProjectRequestBuilder> {
  @BuiltValueField(wireName: r'id')
  String? get id;

  @BuiltValueField(wireName: r'name')
  String get name;

  @BuiltValueField(wireName: r'git_url')
  String? get gitUrl;

  @BuiltValueField(wireName: r'local_path')
  String? get localPath;

  RegisterProjectRequest._();

  factory RegisterProjectRequest([void updates(RegisterProjectRequestBuilder b)]) = _$RegisterProjectRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(RegisterProjectRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<RegisterProjectRequest> get serializer => _$RegisterProjectRequestSerializer();
}

class _$RegisterProjectRequestSerializer implements PrimitiveSerializer<RegisterProjectRequest> {
  @override
  final Iterable<Type> types = const [RegisterProjectRequest, _$RegisterProjectRequest];

  @override
  final String wireName = r'RegisterProjectRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    RegisterProjectRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.id != null) {
      yield r'id';
      yield serializers.serialize(
        object.id,
        specifiedType: const FullType(String),
      );
    }
    yield r'name';
    yield serializers.serialize(
      object.name,
      specifiedType: const FullType(String),
    );
    if (object.gitUrl != null) {
      yield r'git_url';
      yield serializers.serialize(
        object.gitUrl,
        specifiedType: const FullType(String),
      );
    }
    if (object.localPath != null) {
      yield r'local_path';
      yield serializers.serialize(
        object.localPath,
        specifiedType: const FullType(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    RegisterProjectRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required RegisterProjectRequestBuilder result,
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
        case r'git_url':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.gitUrl = valueDes;
          break;
        case r'local_path':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.localPath = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  RegisterProjectRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = RegisterProjectRequestBuilder();
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

