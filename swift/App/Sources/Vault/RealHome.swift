import Darwin
import Foundation

enum RealHome {
    static func url() -> URL {
        if let entry = getpwuid(getuid()), let dir = entry.pointee.pw_dir {
            let path = String(cString: dir)
            if !path.isEmpty { return URL(fileURLWithPath: path, isDirectory: true) }
        }
        return URL(fileURLWithPath: NSHomeDirectory(), isDirectory: true)
    }

    static func homeRelative(_ url: URL, home: URL = RealHome.url()) -> String {
        let path = url.standardizedFileURL.path
        let homePath = home.standardizedFileURL.path
        guard !homePath.isEmpty, homePath != "/" else { return path }
        if path == homePath { return "~" }
        if path.hasPrefix(homePath + "/") { return "~" + path.dropFirst(homePath.count) }
        return path
    }

    static func cliVaultDirectory(home: URL = RealHome.url()) -> URL {
        home.appendingPathComponent(".local/share/tessera", isDirectory: true)
    }

    static func nearestExistingAncestor(of url: URL) -> URL {
        var candidate = url.standardizedFileURL
        while true {
            var info = stat()
            if stat(candidate.path, &info) == 0 {
                if info.st_mode & S_IFMT == S_IFDIR { return candidate }
            } else {
                let code = errno
                // The sandbox answers EPERM for a path that exists outside the container.
                if code == EPERM || code == EACCES { return candidate }
            }
            let parent = candidate.deletingLastPathComponent()
            if parent.path == candidate.path { return candidate }
            candidate = parent
        }
    }
}
