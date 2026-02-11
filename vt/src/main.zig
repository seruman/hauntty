const std = @import("std");
const vt = @import("ghostty-vt");
const Terminal = vt.Terminal;
const TerminalFormatter = vt.formatter.TerminalFormatter;
const FormatterOptions = vt.formatter.Options;

const allocator = std.heap.wasm_allocator;

/// Override std log for wasm32-freestanding — ghostty-vt's own module
/// sets its logFn to the extern "env" log bridge, so Ghostty's internal
/// logging still reaches the host. This only covers our root module.
pub const std_options: std.Options = .{
    .logFn = struct {
        fn f(
            comptime _: std.log.Level,
            comptime _: @TypeOf(.EnumLiteral),
            comptime _: []const u8,
            _: anytype,
        ) void {}
    }.f,
};

// ---------------------------------------------------------------------------
// Type helpers
// ---------------------------------------------------------------------------

fn ReturnType(comptime F: type) type {
    return switch (@typeInfo(F)) {
        .@"fn" => |f| f.return_type.?,
        .pointer => |p| @typeInfo(p.child).@"fn".return_type.?,
        else => @compileError("expected function type"),
    };
}

const VtStream = ReturnType(@TypeOf(Terminal.vtStream));

// ---------------------------------------------------------------------------
// Global state — one terminal per WASM instance
// ---------------------------------------------------------------------------

var g_terminal: ?Terminal = null;
var g_stream: ?VtStream = null;
var g_dump: ?std.Io.Writer.Allocating = null;

// ---------------------------------------------------------------------------
// WASM helpers
// ---------------------------------------------------------------------------

fn wasmSlice(ptr: u32, len: u32) ?[]const u8 {
    if (len == 0) return &.{};
    const p: [*]const u8 = @ptrFromInt(ptr);
    return p[0..len];
}

// ---------------------------------------------------------------------------
// Memory exports
// ---------------------------------------------------------------------------

export fn gx_alloc(len: u32) callconv(.c) u32 {
    const slice = allocator.alloc(u8, len) catch return 0;
    return @intFromPtr(slice.ptr);
}

export fn gx_free(ptr: u32, len: u32) callconv(.c) void {
    if (ptr == 0) return;
    const p: [*]u8 = @ptrFromInt(ptr);
    allocator.free(p[0..len]);
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

export fn gx_init(cols: u32, rows: u32, scrollback: u32) callconv(.c) i32 {
    // Re-init: tear down previous instance
    if (g_stream) |*s| {
        s.deinit();
        g_stream = null;
    }
    if (g_terminal) |*t| {
        t.deinit(allocator);
        g_terminal = null;
    }
    dumpFree();

    g_terminal = Terminal.init(allocator, .{
        .cols = @intCast(cols),
        .rows = @intCast(rows),
        .max_scrollback = scrollback,
    }) catch return -1;

    g_stream = g_terminal.?.vtStream();
    return 0;
}

export fn gx_deinit() callconv(.c) void {
    if (g_stream) |*s| {
        s.deinit();
        g_stream = null;
    }
    if (g_terminal) |*t| {
        t.deinit(allocator);
        g_terminal = null;
    }
    dumpFree();
}

// ---------------------------------------------------------------------------
// Data flow
// ---------------------------------------------------------------------------

export fn gx_feed(ptr: u32, len: u32) callconv(.c) i32 {
    const stream = &(g_stream orelse return -1);
    const data = wasmSlice(ptr, len) orelse return -1;
    stream.nextSlice(data) catch return -1;
    return 0;
}

// ---------------------------------------------------------------------------
// Terminal operations
// ---------------------------------------------------------------------------

export fn gx_resize(cols: u32, rows: u32) callconv(.c) i32 {
    const t = &(g_terminal orelse return -1);
    t.resize(allocator, @intCast(cols), @intCast(rows)) catch return -1;
    return 0;
}

export fn gx_reset() callconv(.c) i32 {
    const t = &(g_terminal orelse return -1);
    // Tear down old stream — fullReset changes internal state
    if (g_stream) |*s| s.deinit();
    t.fullReset();
    g_stream = t.vtStream();
    return 0;
}

// ---------------------------------------------------------------------------
// State extraction — dump screen in plain or VT format
// ---------------------------------------------------------------------------

fn dumpFree() void {
    if (g_dump) |*d| {
        d.deinit();
        g_dump = null;
    }
}

/// Dump the screen. format: 0=plain, 1=vt (for reattach), 2=vt-safe (for display).
export fn gx_dump_screen(format: u32) callconv(.c) i32 {
    const t = &(g_terminal orelse return -1);

    dumpFree();

    g_dump = .init(allocator);

    switch (format) {
        0 => {
            // Plain text — no escape sequences.
            const fmt: TerminalFormatter = .init(t, .{
                .emit = .plain,
                .palette = &t.colors.palette.current,
            });
            g_dump.?.writer.print("{f}", .{fmt}) catch {
                dumpFree();
                return -1;
            };
        },
        1 => {
            // Full VT with all extras — for reattach state restoration.
            var fmt: TerminalFormatter = .init(t, .vt);
            fmt.extra = .all;
            fmt.format(&g_dump.?.writer) catch {
                dumpFree();
                return -1;
            };
        },
        2 => {
            // Safe VT — colors preserved but no palette/mode changes that
            // would corrupt the host terminal. Ends with SGR reset.
            const fmt: TerminalFormatter = .{
                .terminal = t,
                .opts = .{
                    .emit = .vt,
                    .palette = &t.colors.palette.current,
                },
                .content = .{ .selection = null },
                .extra = .none,
                .pin_map = null,
            };
            g_dump.?.writer.print("{f}", .{fmt}) catch {
                dumpFree();
                return -1;
            };
            g_dump.?.writer.writeAll("\x1b[0m") catch {
                dumpFree();
                return -1;
            };
        },
        else => return -1,
    }

    const data = g_dump.?.written();
    return @intCast(data.len);
}

export fn gx_dump_ptr() callconv(.c) u32 {
    if (g_dump == null) return 0;
    const data = g_dump.?.written();
    if (data.len == 0) return 0;
    return @intFromPtr(data.ptr);
}

// ---------------------------------------------------------------------------
// Metadata
// ---------------------------------------------------------------------------

export fn gx_get_cursor_pos() callconv(.c) u32 {
    const t = &(g_terminal orelse return 0);
    const col: u32 = t.screens.active.cursor.x;
    const row: u32 = t.screens.active.cursor.y;
    return col | (row << 16);
}

export fn gx_is_alt_screen() callconv(.c) u32 {
    const t = &(g_terminal orelse return 0);
    return if (t.screens.active_key == .alternate) @as(u32, 1) else @as(u32, 0);
}
