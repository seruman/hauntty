const std = @import("std");
const mem = std.mem;
const fs = std.fs;

pub fn build(b: *std.Build) void {
    const optimize = b.standardOptimizeOption(.{});

    const wasm_query: std.Target.Query = .{
        .cpu_arch = .wasm32,
        .os_tag = .freestanding,
    };

    const dep = b.dependency("ghostty", .{
        .target = wasm_query,
        .optimize = optimize,
    });

    // Apply Ghostty patches before compilation.
    applyPatches(b, dep);

    const exe_mod = b.createModule(.{
        .root_source_file = b.path("src/main.zig"),
        .target = b.resolveTargetQuery(wasm_query),
        .optimize = optimize,
    });

    exe_mod.addImport("ghostty-vt", dep.module("ghostty-vt"));

    const exe = b.addExecutable(.{
        .name = "hauntty-vt",
        .root_module = exe_mod,
    });
    exe.rdynamic = true;
    exe.entry = .disabled;

    b.installArtifact(exe);

    // Copy WASM to ../wasm/ for go:embed.
    const copy = b.addSystemCommand(&.{"cp"});
    copy.addFileArg(exe.getEmittedBin());
    copy.addArg(b.pathFromRoot("../wasm/hauntty-vt.wasm"));
    b.getInstallStep().dependOn(&copy.step);
}

fn applyPatches(b: *std.Build, dep: *std.Build.Dependency) void {
    const patches = .{
        .{
            "ghostty.patch",
            "src/terminal/formatter.zig",
            "@intCast(discarding.count)",
        },
        .{
            "ghostty-report-pwd.patch",
            "src/terminal/stream_readonly.zig",
            "report_pwd => self.terminal.setPwd",
        },
    };

    inline for (patches) |p| {
        const path = dep.path(p[1]).getPath(b);
        if (!fileContains(b.allocator, path, p[2])) {
            const cmd = b.addSystemCommand(&.{
                "patch", "-p1", "-i", b.pathFromRoot(p[0]),
            });
            cmd.setCwd(dep.path("."));
            b.getInstallStep().dependOn(&cmd.step);
        }
    }
}

fn fileContains(gpa: mem.Allocator, path: []const u8, needle: []const u8) bool {
    var handle = if (fs.path.isAbsolute(path))
        fs.openFileAbsolute(path, .{}) catch return false
    else
        fs.cwd().openFile(path, .{}) catch return false;
    defer handle.close();

    const stat = handle.stat() catch return false;
    if (stat.size == 0) return false;

    var buffer: [4096]u8 = undefined;
    var file_reader = handle.reader(&buffer);
    const reader = &file_reader.interface;

    const contents = reader.readAlloc(gpa, stat.size) catch return false;
    defer gpa.free(contents);
    return mem.indexOf(u8, contents, needle) != null;
}
