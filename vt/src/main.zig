const std = @import("std");
const vt = @import("ghostty-vt");
const Terminal = vt.Terminal;
const Selection = vt.Selection;
const TerminalFormatter = vt.formatter.TerminalFormatter;
const ScreenFormatter = vt.formatter.ScreenFormatter;
const FormatterOptions = vt.formatter.Options;
const Key = vt.input.Key;
const KeyEvent = vt.input.KeyEvent;
const KeyMods = vt.input.KeyMods;
const KeyEncodeOptions = vt.input.KeyEncodeOptions;
const encodeKey = vt.input.encodeKey;

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

// Dump format constants — bits 0-3 are format, bit 4 is unwrap flag.
const fmt_plain = 0;
const fmt_vt_full = 1;
const fmt_vt_safe = 2;
const fmt_html = 3;
const format_mask: u32 = 0x0F;
const flag_unwrap: u32 = 0x10;
const flag_scrollback: u32 = 0x20;

fn dumpFree() void {
    if (g_dump) |*d| {
        d.deinit();
        g_dump = null;
    }
}

/// Dump the screen. format_flags: bits 0-3 = format (0=plain, 1=vt-full, 2=vt-safe, 3=html),
/// bit 4 = unwrap, bit 5 = include scrollback (default: visible only).
export fn gx_dump_screen(format_flags: u32) callconv(.c) i32 {
    const t = &(g_terminal orelse return -1);

    const format = format_flags & format_mask;
    const unwrap = (format_flags & flag_unwrap) != 0;
    const scrollback = (format_flags & flag_scrollback) != 0;

    dumpFree();

    g_dump = .init(allocator);

    // Visible-only: restrict content to the active area.
    // Scrollback: null selection = all content including scrollback.
    const active_content: ScreenFormatter.Content = if (scrollback)
        .{ .selection = null }
    else
        .{ .selection = Selection.init(
            t.screens.active.pages.getTopLeft(.active),
            t.screens.active.pages.getBottomRight(.active) orelse
                t.screens.active.pages.getTopLeft(.active),
            false,
        ) };

    switch (format) {
        fmt_plain => {
            // Plain text — no escape sequences.
            var fmt: TerminalFormatter = .init(t, .{
                .emit = .plain,
                .palette = &t.colors.palette.current,
                .unwrap = unwrap,
            });
            fmt.content = active_content;
            g_dump.?.writer.print("{f}", .{fmt}) catch {
                dumpFree();
                return -1;
            };
        },
        fmt_vt_full => {
            // VT for reattach state restoration — always includes scrollback.
            // palette=false: OSC 4 palette sequences override host terminal colors.
            // tabstops=false: tabstop restoration moves cursor after CUP, corrupting position.
            var fmt: TerminalFormatter = .init(t, .{
                .emit = .vt,
                .unwrap = unwrap,
            });
            fmt.content = .{ .selection = null };
            fmt.extra = .{
                .palette = false,
                .modes = true,
                .scrolling_region = true,
                .tabstops = false,
                .pwd = true,
                .keyboard = true,
                .screen = .all,
            };
            fmt.format(&g_dump.?.writer) catch {
                dumpFree();
                return -1;
            };
        },
        fmt_vt_safe => {
            // Safe VT — colors preserved but no palette/mode changes that
            // would corrupt the host terminal. Ends with SGR reset.
            const fmt: TerminalFormatter = .{
                .terminal = t,
                .opts = .{
                    .emit = .vt,
                    .palette = &t.colors.palette.current,
                    .unwrap = unwrap,
                },
                .content = active_content,
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
        fmt_html => {
            var fmt: TerminalFormatter = .init(t, .{
                .emit = .html,
                .palette = &t.colors.palette.current,
                .unwrap = unwrap,
            });
            fmt.content = active_content;
            g_dump.?.writer.print("{f}", .{fmt}) catch {
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
// Key encoding — encode a synthetic key event using terminal mode state
// ---------------------------------------------------------------------------

// Key code constants shared with Go. ASCII printable range (0x20-0x7E)
// maps via Key.fromASCII. Named keys use 0x100+.
const hk_enter: u32 = 0x100;
const hk_escape: u32 = 0x101;
const hk_tab: u32 = 0x102;
const hk_backspace: u32 = 0x103;
const hk_up: u32 = 0x110;
const hk_down: u32 = 0x111;
const hk_left: u32 = 0x112;
const hk_right: u32 = 0x113;
const hk_home: u32 = 0x120;
const hk_end: u32 = 0x121;
const hk_page_up: u32 = 0x122;
const hk_page_down: u32 = 0x123;
const hk_insert: u32 = 0x124;
const hk_delete: u32 = 0x125;
const hk_f1: u32 = 0x130;
const hk_f2: u32 = 0x131;
const hk_f3: u32 = 0x132;
const hk_f4: u32 = 0x133;
const hk_f5: u32 = 0x134;
const hk_f6: u32 = 0x135;
const hk_f7: u32 = 0x136;
const hk_f8: u32 = 0x137;
const hk_f9: u32 = 0x138;
const hk_f10: u32 = 0x139;
const hk_f11: u32 = 0x13A;
const hk_f12: u32 = 0x13B;

const KeyInfo = struct {
    key: Key,
    codepoint: u21,
};

fn keyFromCode(code: u32) ?KeyInfo {
    if (code >= 0x20 and code <= 0x7E) {
        const ch: u8 = @intCast(code);
        const k = Key.fromASCII(ch) orelse return null;
        return .{ .key = k, .codepoint = @intCast(code) };
    }
    return switch (code) {
        hk_enter => .{ .key = .enter, .codepoint = '\r' },
        hk_escape => .{ .key = .escape, .codepoint = 0x1B },
        hk_tab => .{ .key = .tab, .codepoint = '\t' },
        hk_backspace => .{ .key = .backspace, .codepoint = 0x7F },
        hk_up => .{ .key = .arrow_up, .codepoint = 0 },
        hk_down => .{ .key = .arrow_down, .codepoint = 0 },
        hk_left => .{ .key = .arrow_left, .codepoint = 0 },
        hk_right => .{ .key = .arrow_right, .codepoint = 0 },
        hk_home => .{ .key = .home, .codepoint = 0 },
        hk_end => .{ .key = .end, .codepoint = 0 },
        hk_page_up => .{ .key = .page_up, .codepoint = 0 },
        hk_page_down => .{ .key = .page_down, .codepoint = 0 },
        hk_insert => .{ .key = .insert, .codepoint = 0 },
        hk_delete => .{ .key = .delete, .codepoint = 0 },
        hk_f1 => .{ .key = .f1, .codepoint = 0 },
        hk_f2 => .{ .key = .f2, .codepoint = 0 },
        hk_f3 => .{ .key = .f3, .codepoint = 0 },
        hk_f4 => .{ .key = .f4, .codepoint = 0 },
        hk_f5 => .{ .key = .f5, .codepoint = 0 },
        hk_f6 => .{ .key = .f6, .codepoint = 0 },
        hk_f7 => .{ .key = .f7, .codepoint = 0 },
        hk_f8 => .{ .key = .f8, .codepoint = 0 },
        hk_f9 => .{ .key = .f9, .codepoint = 0 },
        hk_f10 => .{ .key = .f10, .codepoint = 0 },
        hk_f11 => .{ .key = .f11, .codepoint = 0 },
        hk_f12 => .{ .key = .f12, .codepoint = 0 },
        else => null,
    };
}

var g_key_utf8: [4]u8 = undefined;

/// Encode a synthetic key press using the terminal's current mode state.
/// key_code: ASCII codepoint (0x20-0x7E) or named key constant (0x100+).
/// mods_int: modifier bitmask (bit 0=shift, 1=ctrl, 2=alt, 3=super).
/// Returns encoded byte count (read via gx_dump_ptr), or negative on error.
export fn gx_encode_key(key_code: u32, mods_int: u32) callconv(.c) i32 {
    const t = &(g_terminal orelse return -1);
    const info = keyFromCode(key_code) orelse return -2;

    const mods: KeyMods = @bitCast(@as(u16, @intCast(mods_int & 0xFFFF)));

    var event: KeyEvent = .{
        .action = .press,
        .key = info.key,
        .mods = mods,
        .unshifted_codepoint = info.codepoint,
    };

    // For printable ASCII keys, set utf8 so the encoder can use it.
    if (info.codepoint >= 0x20 and info.codepoint < 0x7F) {
        const ch: u8 = @intCast(info.codepoint);
        if (mods.shift and ch >= 'a' and ch <= 'z') {
            g_key_utf8[0] = ch - 0x20;
            event.utf8 = g_key_utf8[0..1];
            event.consumed_mods = .{ .shift = true };
        } else if (!mods.ctrl) {
            g_key_utf8[0] = ch;
            event.utf8 = g_key_utf8[0..1];
        }
    }

    dumpFree();
    g_dump = .init(allocator);

    const opts = KeyEncodeOptions.fromTerminal(t);
    encodeKey(&g_dump.?.writer, event, opts) catch {
        dumpFree();
        return -1;
    };

    const data = g_dump.?.written();
    return @intCast(data.len);
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

export fn gx_get_pwd_len() callconv(.c) u32 {
    const t = &(g_terminal orelse return 0);
    const pwd = t.getPwd() orelse return 0;
    return @intCast(pwd.len);
}

export fn gx_get_pwd_ptr() callconv(.c) u32 {
    const t = &(g_terminal orelse return 0);
    const pwd = t.getPwd() orelse return 0;
    return @intFromPtr(pwd.ptr);
}
