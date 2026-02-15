const std = @import("std");

pub fn build(b: *std.Build) void {
    const optimize = b.standardOptimizeOption(.{});

    const wasm_query: std.Target.Query = .{
        .cpu_arch = .wasm32,
        .os_tag = .freestanding,
    };

    // Apply Ghostty patches before compilation.
    const patch_step = applyPatches(b);

    const exe_mod = b.createModule(.{
        .root_source_file = b.path("src/main.zig"),
        .target = b.resolveTargetQuery(wasm_query),
        .optimize = optimize,
    });

    if (b.lazyDependency("ghostty", .{
        .target = wasm_query,
        .optimize = optimize,
    })) |dep| {
        exe_mod.addImport(
            "ghostty-vt",
            dep.module("ghostty-vt"),
        );
    }

    const exe = b.addExecutable(.{
        .name = "hauntty-vt",
        .root_module = exe_mod,
    });
    exe.rdynamic = true;
    exe.entry = .disabled;
    exe.step.dependOn(patch_step);

    b.installArtifact(exe);
}

fn applyPatches(b: *std.Build) *std.Build.Step {
    const ghostty_path = b.pathFromRoot("../x/ghostty");
    const patch_dir = b.pathFromRoot(".");

    const step = b.addSystemCommand(&.{
        "sh", "-c",
        \\for patch in "$1"/ghostty*.patch; do
        \\    [ -f "$patch" ] || continue
        \\    if git -C "$2" apply --check "$patch" 2>/dev/null; then
        \\        git -C "$2" apply "$patch"
        \\    fi
        \\done
        ,
        "--",
    });
    step.addArg(patch_dir);
    step.addArg(ghostty_path);

    return &step.step;
}
