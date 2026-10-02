import 'ffmpeg_session.dart';
import 'return_code.dart';

extension XFFmpegSession on Future<FFmpegSession> {
  Future<String> thenReturnResultOrLogs(
    String Function(FFmpegSession) onSuccess,
  ) =>
      then(
        (session) => session.getReturnCode().then(
          (returnCode) {
            if (ReturnCode.isSuccess(returnCode)) {
              return onSuccess(session);
            }
            return session.getAllLogs().then(
              (logs) {
                final String logOutput =
                    logs.map((log) => log.getMessage()).join('\n');
                throw Exception(
                  'FFmpeg command failed with return code: $returnCode. Logs: $logOutput',
                );
              },
            );
          },
        ),
      );
}
