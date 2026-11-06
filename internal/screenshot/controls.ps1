# Owner-drawn surface of the workspace. Compiled once at start; capture.ps1 passes it to Add-Type.
# Windows PowerShell 5.1 compiles with C# 5: no ?., $"" or expression-bodied members.
$script:controlsSource=@'
using System;
using System.Collections.Generic;
using System.Drawing;
using System.Drawing.Drawing2D;
using System.Drawing.Imaging;
using System.Runtime.InteropServices;
using System.Windows.Forms;

public static class CaptureDpi {
 public static System.Threading.Tasks.Task<string> ReadReply() { return System.Threading.Tasks.Task.Run(() => Console.In.ReadLine()); }
 [DllImport("user32.dll")] public static extern bool SetProcessDpiAwarenessContext(IntPtr value);
 [DllImport("user32.dll")] static extern IntPtr MonitorFromPoint(POINT pt, uint flags);
 [DllImport("shcore.dll")] static extern int GetDpiForMonitor(IntPtr monitor, int type, out uint x, out uint y);
 [DllImport("dwmapi.dll")] static extern int DwmSetWindowAttribute(IntPtr hwnd, int attribute, ref int value, int size);
 [StructLayout(LayoutKind.Sequential)] struct POINT { public int X; public int Y; }
 /// <summary>Scale of the monitor under a physical screen point; 1 means 96 DPI.</summary>
 public static float ScaleAt(int x, int y) {
  uint dx = 96, dy = 96;
  try {
   POINT p; p.X = x; p.Y = y;
   if (GetDpiForMonitor(MonitorFromPoint(p, 2), 0, out dx, out dy) != 0) { dx = 96; }
  } catch (DllNotFoundException) { dx = 96; } catch (EntryPointNotFoundException) { dx = 96; }
  return dx / 96f;
 }
 /// <summary>Rounded corners on Windows 11; ignored by older systems.</summary>
 public static void RoundCorners(IntPtr hwnd) { int round = 2; try { DwmSetWindowAttribute(hwnd, 33, ref round, 4); } catch (DllNotFoundException) { } }
 public static void DarkCaption(IntPtr hwnd, bool dark) { int on = dark ? 1 : 0; try { DwmSetWindowAttribute(hwnd, 20, ref on, 4); } catch (DllNotFoundException) { } }
}

/// <summary>The main window's light, dark and pink tokens (src/style.css), flattened onto their surface.</summary>
public sealed class ShotPalette {
 public Color Card, Control, ControlStroke, Stroke, Fg, Muted, Accent, Err;
 public bool Dark;
 static Color Over(int a, int r, int g, int b, Color on) {
  return Color.FromArgb((r * a + on.R * (255 - a)) / 255, (g * a + on.G * (255 - a)) / 255, (b * a + on.B * (255 - a)) / 255);
 }
 public static ShotPalette For(string theme) {
  ShotPalette p = new ShotPalette();
  p.Accent = Color.FromArgb(10, 132, 255); p.Err = Color.FromArgb(215, 45, 35);
  if (theme == "dark") {
   p.Dark = true; p.Err = Color.FromArgb(255, 105, 97);
   p.Card = Color.FromArgb(35, 35, 35); p.Control = Color.FromArgb(44, 45, 49);
   p.ControlStroke = Over(71, 120, 140, 200, p.Control); p.Stroke = Over(20, 255, 255, 255, p.Card);
   p.Fg = Color.FromArgb(236, 236, 238); p.Muted = Color.FromArgb(154, 157, 164);
  } else if (theme == "pink") {
   p.Card = Color.FromArgb(255, 251, 252); p.Control = Color.FromArgb(255, 243, 247);
   p.ControlStroke = Color.FromArgb(240, 207, 217); p.Stroke = p.ControlStroke;
   p.Fg = Color.FromArgb(74, 48, 57); p.Muted = Color.FromArgb(138, 102, 114); p.Accent = Color.FromArgb(255, 105, 180);
  } else {
   p.Card = Color.FromArgb(250, 250, 250); p.Control = Color.FromArgb(233, 234, 238);
   p.ControlStroke = Over(46, 60, 80, 130, p.Control); p.Stroke = Over(23, 0, 0, 0, p.Card);
   p.Fg = Color.FromArgb(29, 29, 31); p.Muted = Color.FromArgb(107, 110, 117);
  }
  return p;
 }
}

public static class ShotPaint {
 public const string Family = "Microsoft YaHei UI";
 public static Font FontPx(float px, FontStyle style) { return new Font(Family, px, style, GraphicsUnit.Pixel); }
 public static GraphicsPath Round(RectangleF r, float radius) {
  float d = Math.Min(radius * 2, Math.Min(r.Width, r.Height));
  GraphicsPath p = new GraphicsPath();
  p.AddArc(r.X, r.Y, d, d, 180, 90); p.AddArc(r.Right - d, r.Y, d, d, 270, 90);
  p.AddArc(r.Right - d, r.Bottom - d, d, d, 0, 90); p.AddArc(r.X, r.Bottom - d, d, d, 90, 90);
  p.CloseFigure();
  return p;
 }
 public static void Fill(Graphics g, RectangleF r, float radius, Color fill, Color stroke, float width) {
  using (GraphicsPath path = Round(r, radius)) {
   using (SolidBrush b = new SolidBrush(fill)) { g.FillPath(b, path); }
   if (width > 0) { using (Pen pen = new Pen(stroke, width)) { g.DrawPath(pen, path); } }
  }
 }
 public static void Shadow(Graphics g, Rectangle card, float radius, float scale) {
  // Layered outlines read as one soft shadow; GDI+ has no blur.
  for (int i = 7; i >= 1; i--) {
   float spread = i * 1.7f * scale;
   RectangleF r = new RectangleF(card.X - spread, card.Y - spread + 3 * scale, card.Width + spread * 2, card.Height + spread * 2);
   using (GraphicsPath path = Round(r, radius + spread)) using (SolidBrush b = new SolidBrush(Color.FromArgb(5, 0, 0, 0))) { g.FillPath(b, path); }
  }
 }
 // Icons on a 24-unit grid, one polyline per array; a leading 1 closes the figure.
 static float[][] Glyph(string name) {
  switch (name) {
   case "extract": return new float[][] { new float[] { 0, 4, 8, 4, 4, 8, 4 }, new float[] { 0, 16, 4, 20, 4, 20, 8 }, new float[] { 0, 20, 16, 20, 20, 16, 20 }, new float[] { 0, 8, 20, 4, 20, 4, 16 }, new float[] { 0, 8, 9.5f, 16, 9.5f }, new float[] { 0, 8, 12.5f, 16, 12.5f }, new float[] { 0, 8, 15.5f, 13, 15.5f } };
   case "text": return new float[][] { new float[] { 1, 6, 3.5f, 14.5f, 3.5f, 19, 8, 19, 20.5f, 6, 20.5f }, new float[] { 0, 14, 3.5f, 14, 8.5f, 19, 8.5f }, new float[] { 0, 9.5f, 13, 16, 13 }, new float[] { 0, 9.5f, 16.5f, 14, 16.5f } };
   case "pin": return new float[][] { new float[] { 0, 12, 16.5f, 12, 21 }, new float[] { 1, 9, 3.5f, 15, 3.5f, 14, 9, 17.5f, 13, 17.5f, 15, 6.5f, 15, 6.5f, 13, 10, 9 } };
   case "reselect": return new float[][] { new float[] { 0, 4, 9, 4, 4, 9, 4 }, new float[] { 0, 15, 4, 20, 4, 20, 9 }, new float[] { 0, 20, 15, 20, 20, 15, 20 }, new float[] { 0, 9, 20, 4, 20, 4, 15 } };
   case "translate": return new float[][] { new float[] { 0, 3.5f, 6, 12.5f, 6 }, new float[] { 0, 8, 4, 8, 6 }, new float[] { 0, 10.5f, 6, 9.2f, 9.5f, 7, 12.3f, 4, 14.5f }, new float[] { 0, 5.5f, 9.5f, 7.5f, 12, 11, 14.5f }, new float[] { 0, 13, 21, 17, 11, 21, 21 }, new float[] { 0, 14.3f, 18, 19.7f, 18 } };
   case "copy": return new float[][] { new float[] { 1, 9, 9, 20, 9, 20, 20, 9, 20 }, new float[] { 0, 5.5f, 15, 4, 15, 4, 4, 15, 4, 15, 5.5f } };
   case "close": return new float[][] { new float[] { 0, 6, 6, 18, 18 }, new float[] { 0, 18, 6, 6, 18 } };
   case "check": return new float[][] { new float[] { 0, 5, 12.5f, 9.5f, 17, 19, 7 } };
   case "chevron": return new float[][] { new float[] { 0, 6, 9, 12, 15, 18, 9 } };
   case "arrow": return new float[][] { new float[] { 0, 5, 12, 19, 12 }, new float[] { 0, 13, 6, 19, 12, 13, 18 } };
  }
  return new float[0][];
 }
 public static void Icon(Graphics g, string name, RectangleF box, Color color, float width) {
  float unit = box.Width / 24f;
  using (Pen pen = new Pen(color, width)) {
   pen.StartCap = LineCap.Round; pen.EndCap = LineCap.Round; pen.LineJoin = LineJoin.Round;
   foreach (float[] line in Glyph(name)) {
    PointF[] points = new PointF[(line.Length - 1) / 2];
    for (int i = 0; i < points.Length; i++) { points[i] = new PointF(box.X + line[1 + i * 2] * unit, box.Y + line[2 + i * 2] * unit); }
    if (line[0] == 1) { g.DrawPolygon(pen, points); } else { g.DrawLines(pen, points); }
   }
  }
 }
}

/// <summary>Full-screen surface: dimmed frozen desktop, the clear selection and the idle hint.</summary>
public class ShotForm : Form {
 public ShotPalette Palette;
 public Bitmap Frozen; public Bitmap Dimmed; public Bitmap Overlay;
 public Rectangle Selection = Rectangle.Empty;
 string hintTitle = ""; string hintDetail = ""; Rectangle hintRect = Rectangle.Empty; float hintScale = 1;
 public float SelectionScale = 1;
 public ShotForm() {
  SetStyle(ControlStyles.UserPaint | ControlStyles.AllPaintingInWmPaint | ControlStyles.OptimizedDoubleBuffer | ControlStyles.Opaque, true);
 }
 public void Freeze(Bitmap screen, int shade) {
  Frozen = screen;
  Dimmed = new Bitmap(screen.Width, screen.Height, PixelFormat.Format32bppPArgb);
  using (Graphics g = Graphics.FromImage(Dimmed)) {
   g.CompositingMode = CompositingMode.SourceCopy;
   g.DrawImageUnscaled(screen, 0, 0);
   g.CompositingMode = CompositingMode.SourceOver;
   using (SolidBrush b = new SolidBrush(Color.FromArgb(shade, 0, 0, 0))) { g.FillRectangle(b, 0, 0, Dimmed.Width, Dimmed.Height); }
  }
 }
 static Rectangle Grow(Rectangle r, int by) { r.Inflate(by, by); return r; }
 public void SetSelection(Rectangle next) {
  int pad = (int)Math.Ceiling(3 * SelectionScale);
  if (!Selection.IsEmpty) { Invalidate(Grow(Selection, pad)); }
  Selection = next;
  if (!Selection.IsEmpty) { Invalidate(Grow(Selection, pad)); }
 }
 public void SetOverlay(Bitmap image) {
  Overlay = image;
  if (!Selection.IsEmpty) { Invalidate(Selection); }
 }
 /// <summary>Shows the hint on a work area given in this surface's coordinates; an empty title hides it.</summary>
 public void SetHint(string title, string detail, Rectangle area, float scale) {
  if (!hintRect.IsEmpty) { Invalidate(hintRect); }
  hintTitle = title; hintDetail = detail; hintScale = scale;
  if (string.IsNullOrEmpty(title)) { hintRect = Rectangle.Empty; return; }
  using (Font big = ShotPaint.FontPx(15 * scale, FontStyle.Bold)) using (Font small = ShotPaint.FontPx(13 * scale, FontStyle.Regular)) {
   Size a = TextRenderer.MeasureText(title, big); Size b = TextRenderer.MeasureText(detail, small);
   int w = Math.Min(Math.Max(a.Width, b.Width) + (int)(36 * scale), area.Width - (int)(24 * scale));
   int h = a.Height + b.Height + (int)(26 * scale);
   Rectangle card = new Rectangle(area.X + (area.Width - w) / 2, area.Y + (int)(28 * scale), w, h);
   hintRect = Grow(card, (int)(16 * scale));
  }
  Invalidate(hintRect);
 }
 public void PaintScene(Graphics g, Rectangle clip) {
  g.CompositingMode = CompositingMode.SourceCopy;
  g.InterpolationMode = InterpolationMode.NearestNeighbor;
  g.PixelOffsetMode = PixelOffsetMode.Half;
  if (Dimmed != null) { g.DrawImage(Dimmed, clip, clip, GraphicsUnit.Pixel); }
  if (Frozen != null && !Selection.IsEmpty) {
   Rectangle clear = Rectangle.Intersect(clip, Selection);
   if (!clear.IsEmpty) {
    if (Overlay != null) {
     Rectangle source = clear; source.Offset(-Selection.X, -Selection.Y);
     g.DrawImage(Overlay, clear, source, GraphicsUnit.Pixel);
    } else { g.DrawImage(Frozen, clear, clear, GraphicsUnit.Pixel); }
   }
  }
  g.CompositingMode = CompositingMode.SourceOver;
  g.PixelOffsetMode = PixelOffsetMode.Default;
  if (!Selection.IsEmpty && Palette != null) {
   int w = (int)Math.Ceiling(2 * SelectionScale);
   using (SolidBrush b = new SolidBrush(Palette.Accent)) {
    g.FillRectangle(b, Selection.X - w, Selection.Y - w, Selection.Width + w * 2, w);
    g.FillRectangle(b, Selection.X - w, Selection.Bottom, Selection.Width + w * 2, w);
    g.FillRectangle(b, Selection.X - w, Selection.Y, w, Selection.Height);
    g.FillRectangle(b, Selection.Right, Selection.Y, w, Selection.Height);
   }
  }
 }
 protected override void OnPaintBackground(PaintEventArgs e) { }
 protected override void OnPaint(PaintEventArgs e) {
  Graphics g = e.Graphics;
  PaintScene(g, e.ClipRectangle);
  if (!hintRect.IsEmpty && Palette != null && e.ClipRectangle.IntersectsWith(hintRect)) {
   int inset = (int)(16 * hintScale);
   Rectangle card = Rectangle.Inflate(hintRect, -inset, -inset);
   g.SmoothingMode = SmoothingMode.AntiAlias;
   ShotPaint.Shadow(g, card, 12 * hintScale, hintScale);
   ShotPaint.Fill(g, card, 12 * hintScale, Palette.Card, Palette.Stroke, Math.Max(1, hintScale));
   g.SmoothingMode = SmoothingMode.None;
   using (Font big = ShotPaint.FontPx(15 * hintScale, FontStyle.Bold)) using (Font small = ShotPaint.FontPx(13 * hintScale, FontStyle.Regular)) {
    int top = card.Y + (int)(11 * hintScale);
    int bigH = TextRenderer.MeasureText(hintTitle, big).Height;
    Rectangle first = new Rectangle(card.X + (int)(12 * hintScale), top, card.Width - (int)(24 * hintScale), bigH);
    Rectangle second = new Rectangle(first.X, top + bigH + (int)(2 * hintScale), first.Width, card.Bottom - top - bigH);
    TextFormatFlags f = TextFormatFlags.HorizontalCenter | TextFormatFlags.EndEllipsis | TextFormatFlags.NoPrefix;
    TextRenderer.DrawText(g, hintTitle, big, first, Palette.Fg, Palette.Card, f);
    TextRenderer.DrawText(g, hintDetail, small, second, Palette.Muted, Palette.Card, f);
   }
  }
  base.OnPaint(e);
 }
}

public interface IShotScaled { void SetScale(float scale); void Theme(ShotPalette palette); }

public enum ShotKind { Primary, Soft, Ghost, Quiet }

public class ShotButton : Button, IShotScaled {
 public ShotKind Kind = ShotKind.Ghost;
 public string Glyph = "";
 /// <summary>Shown in place of the words when the toolbar is compact, for buttons that normally have no icon.</summary>
 public string CompactGlyph = "";
 public bool IconOnly;
 public bool CanCompact { get { return Glyph.Length > 0 || CompactGlyph.Length > 0; } }
 string CurrentGlyph { get { return IconOnly && Glyph.Length == 0 ? CompactGlyph : Glyph; } }
 protected ShotPalette palette; protected float scale = 1;
 protected bool hover; protected bool down;
 public ShotButton() {
  SetStyle(ControlStyles.UserPaint | ControlStyles.AllPaintingInWmPaint | ControlStyles.OptimizedDoubleBuffer | ControlStyles.ResizeRedraw, true);
  Cursor = Cursors.Default;
  UseMnemonic = false;
 }
 public void Theme(ShotPalette p) { palette = p; BackColor = p.Card; ForeColor = p.Fg; }
 public int FixedHeight;
 public virtual void SetScale(float s) { scale = s; Font = ShotPaint.FontPx((FixedHeight > 0 ? 14 : 13) * s, FontStyle.Regular); }
 public virtual int RowHeight() { return (int)((FixedHeight > 0 ? FixedHeight : 32) * scale); }
 public virtual int RowWidth() {
  int icon = CurrentGlyph.Length > 0 ? (int)(16 * scale) : 0;
  if (IconOnly && icon > 0) { return (int)(32 * scale); }
  int text = TextRenderer.MeasureText(Text, Font, Size.Empty, TextFormatFlags.NoPrefix).Width;
  int gap = icon > 0 ? (int)(6 * scale) : 0;
  return icon + gap + text + (int)((FixedHeight > 0 ? 26 : 16) * scale);
 }
 protected override void OnMouseEnter(EventArgs e) { hover = true; Invalidate(); base.OnMouseEnter(e); }
 protected override void OnMouseLeave(EventArgs e) { hover = false; down = false; Invalidate(); base.OnMouseLeave(e); }
 protected override void OnMouseDown(MouseEventArgs e) { down = true; Invalidate(); base.OnMouseDown(e); }
 protected override void OnMouseUp(MouseEventArgs e) { down = false; Invalidate(); base.OnMouseUp(e); }
 protected override void OnEnabledChanged(EventArgs e) { Invalidate(); base.OnEnabledChanged(e); }
 protected override void OnPaintBackground(PaintEventArgs e) { }
 protected Color Dim(Color c) { return Enabled ? c : Color.FromArgb(palette.Dark ? 96 : 110, c); }
 protected static Color Mix(Color a, Color b, float t) { return Color.FromArgb((int)(a.R + (b.R - a.R) * t), (int)(a.G + (b.G - a.G) * t), (int)(a.B + (b.B - a.B) * t)); }
 protected void Ring(Graphics g, RectangleF r, float radius) {
  if (Focused && ShowFocusCues && palette != null) {
   using (GraphicsPath path = ShotPaint.Round(new RectangleF(r.X + scale, r.Y + scale, r.Width - 2 * scale, r.Height - 2 * scale), radius)) using (Pen pen = new Pen(Kind == ShotKind.Primary ? palette.Fg : palette.Accent, 2 * scale)) { g.DrawPath(pen, path); }
  }
 }
 protected override void OnPaint(PaintEventArgs e) {
  if (palette == null) { return; }
  Graphics g = e.Graphics;
  g.Clear(palette.Card);
  g.SmoothingMode = SmoothingMode.AntiAlias;
  RectangleF r = new RectangleF(0.5f, 0.5f, Width - 1, Height - 1);
  float radius = 8 * scale;
  Color text = palette.Fg; Color glyph = palette.Muted;
  if (Kind == ShotKind.Primary) {
   Color fill = Mix(palette.Accent, palette.Dark ? Color.White : Color.Black, down ? .14f : hover && Enabled ? .07f : 0);
   ShotPaint.Fill(g, r, radius, Enabled ? fill : palette.Control, fill, 0);
   text = Enabled ? Color.White : palette.Muted; glyph = text;
  } else if (Kind == ShotKind.Soft) {
   ShotPaint.Fill(g, r, radius, hover && Enabled ? Mix(palette.Control, palette.Fg, down ? .14f : .07f) : palette.Control, palette.ControlStroke, Math.Max(1, scale));
   glyph = palette.Fg;
  } else {
   if (Enabled && (hover || down)) { ShotPaint.Fill(g, r, radius, Mix(palette.Control, palette.Fg, down ? .08f : 0), palette.Control, 0); }
   if (Kind == ShotKind.Quiet) { text = palette.Muted; }
  }
  if (Kind != ShotKind.Primary) { text = Dim(text); glyph = Dim(glyph); }
  string glyphName = CurrentGlyph;
  int icon = glyphName.Length > 0 ? (int)(16 * scale) : 0;
  bool only = IconOnly && icon > 0;
  int textW = only ? 0 : TextRenderer.MeasureText(Text, Font, Size.Empty, TextFormatFlags.NoPrefix).Width;
  int gap = icon > 0 && !only ? (int)(6 * scale) : 0;
  int x = (Width - icon - gap - textW) / 2;
  if (icon > 0) { ShotPaint.Icon(g, glyphName, new RectangleF(x, (Height - icon) / 2f, icon, icon), glyph, 1.6f * scale); }
  Ring(g, r, radius);
  g.SmoothingMode = SmoothingMode.None;
  if (!only) {
   Color back = Kind == ShotKind.Primary ? (Enabled ? palette.Accent : palette.Control) : Kind == ShotKind.Soft ? palette.Control : palette.Card;
   Rectangle area = new Rectangle(x + icon + gap, 0, textW + 2, Height);
   TextFormatFlags f = TextFormatFlags.VerticalCenter | TextFormatFlags.NoPrefix | TextFormatFlags.NoPadding | TextFormatFlags.SingleLine;
   TextRenderer.DrawText(g, Text, Font, area, Enabled || Kind == ShotKind.Primary ? (text.A < 255 ? Mix(back, Color.FromArgb(text.R, text.G, text.B), text.A / 255f) : text) : Mix(back, Color.FromArgb(text.R, text.G, text.B), text.A / 255f), f);
  }
 }
}

/// <summary>A language pill drawn like the main toolbar's, with a themed menu. Mirrors ComboBox's selection API.</summary>
public class ShotPicker : ShotButton {
 public string Label = "";
 public bool Compact;
 public List<string> Items = new List<string>();
 int selected = -1;
 public event EventHandler SelectedIndexChanged;
 public ShotPicker() { Kind = ShotKind.Soft; }
 public int SelectedIndex {
  get { return selected; }
  set {
   if (value == selected) { return; }
   selected = value;
   Text = selected >= 0 && selected < Items.Count ? Items[selected] : "";
   AccessibleName = Label + " " + Text;
   Invalidate();
   EventHandler changed = SelectedIndexChanged;
   if (changed != null) { changed(this, EventArgs.Empty); }
  }
 }
 public string SelectedItem {
  get { return selected >= 0 && selected < Items.Count ? Items[selected] : null; }
  set { SelectedIndex = Items.IndexOf(value); }
 }
 public override void SetScale(float s) { scale = s; Font = ShotPaint.FontPx(14 * s, FontStyle.Regular); }
 public override int RowHeight() { return (int)(36 * scale); }
 public override int RowWidth() {
  int value = TextRenderer.MeasureText(Text, Font, Size.Empty, TextFormatFlags.NoPrefix).Width;
  if (Compact) { return value + (int)(40 * scale); }
  using (Font small = ShotPaint.FontPx(13 * scale, FontStyle.Regular)) {
   int label = TextRenderer.MeasureText(Label, small, Size.Empty, TextFormatFlags.NoPrefix).Width;
   return label + value + (int)(48 * scale);
  }
 }
 protected override void OnClick(EventArgs e) {
  base.OnClick(e);
  int itemWidth = Math.Max(Width, (int)(180 * scale));
  int itemHeight = (int)(32 * scale);
  int inset = (int)(5 * scale);
  ToolStripDropDown drop = new ToolStripDropDown();
  drop.Renderer = new ShotMenuRenderer(palette);
  drop.BackColor = palette.Card;
  drop.Padding = new Padding(inset);
  drop.DropShadowEnabled = true;
  ListBox list = new ListBox();
  list.DrawMode = DrawMode.OwnerDrawFixed;
  list.ItemHeight = itemHeight;
  list.IntegralHeight = false;
  list.BorderStyle = BorderStyle.None;
  list.BackColor = palette.Card;
  list.ForeColor = palette.Fg;
  list.Font = ShotPaint.FontPx(14 * scale, FontStyle.Regular);
  list.Items.AddRange(Items.ToArray());
  list.SelectedIndex = selected;
  list.DrawItem += delegate(object o, DrawItemEventArgs d) { DrawMenuItem(list, d); };
  list.MouseMove += delegate(object o, MouseEventArgs m) {
   int index = list.IndexFromPoint(m.Location);
   if (index >= 0 && index != list.SelectedIndex) { list.SelectedIndex = index; }
  };
  MethodInvoker commit = delegate { int index = list.SelectedIndex; drop.Close(); if (index >= 0) { SelectedIndex = index; } };
  list.MouseClick += delegate { commit(); };
  list.KeyDown += delegate(object o, KeyEventArgs k) { if (k.KeyCode == Keys.Return) { k.Handled = true; commit(); } };
  // A themed list can't restyle its scrollbar, so it is clipped off: the wheel and arrow keys still
  // scroll, and the half-visible last row shows there is more.
  int listHeight = Math.Min(Items.Count * itemHeight, (int)(368 * scale));
  Panel clip = new Panel();
  clip.BackColor = palette.Card;
  clip.Size = new Size(itemWidth, listHeight);
  list.Bounds = new Rectangle(0, 0, itemWidth + SystemInformation.VerticalScrollBarWidth, listHeight);
  clip.Controls.Add(list);
  ToolStripControlHost host = new ToolStripControlHost(clip);
  host.AutoSize = false; host.Margin = Padding.Empty; host.Padding = Padding.Empty;
  host.Size = clip.Size;
  drop.Items.Add(host);
  drop.Closed += delegate { BeginInvoke(new MethodInvoker(drop.Dispose)); };
  CaptureDpi.RoundCorners(drop.Handle);
  drop.Show(this, new Point(0, Height + (int)(4 * scale)));
  list.Focus();
 }
 void DrawMenuItem(ListBox list, DrawItemEventArgs e) {
  if (e.Index < 0) { return; }
  Graphics g = e.Graphics;
  using (SolidBrush b = new SolidBrush(palette.Card)) { g.FillRectangle(b, e.Bounds); }
  bool hot = (e.State & DrawItemState.Selected) != 0;
  bool current = e.Index == selected;
  Color back = hot ? palette.Control : palette.Card;
  g.SmoothingMode = SmoothingMode.AntiAlias;
  if (hot) { ShotPaint.Fill(g, new RectangleF(e.Bounds.X, e.Bounds.Y + scale, e.Bounds.Width - 1, e.Bounds.Height - 2 * scale), 6 * scale, back, back, 0); }
  if (current) {
   int size = (int)(14 * scale);
   ShotPaint.Icon(g, "check", new RectangleF(e.Bounds.Right - size - 8 * scale, e.Bounds.Y + (e.Bounds.Height - size) / 2f, size, size), palette.Accent, 1.8f * scale);
  }
  g.SmoothingMode = SmoothingMode.None;
  Rectangle area = new Rectangle(e.Bounds.X + (int)(10 * scale), e.Bounds.Y, e.Bounds.Width - (int)(36 * scale), e.Bounds.Height);
  TextRenderer.DrawText(g, list.Items[e.Index].ToString(), list.Font, area, current ? palette.Accent : palette.Fg, back, TextFormatFlags.VerticalCenter | TextFormatFlags.NoPrefix | TextFormatFlags.SingleLine | TextFormatFlags.EndEllipsis | TextFormatFlags.NoPadding);
 }
 protected override void OnPaint(PaintEventArgs e) {
  if (palette == null) { return; }
  Graphics g = e.Graphics;
  g.Clear(palette.Card);
  g.SmoothingMode = SmoothingMode.AntiAlias;
  RectangleF r = new RectangleF(0.5f, 0.5f, Width - 1, Height - 1);
  float radius = 10 * scale;
  ShotPaint.Fill(g, r, radius, hover && Enabled ? Mix(palette.Control, palette.Fg, down ? .12f : .06f) : palette.Control, palette.ControlStroke, Math.Max(1, scale));
  int size = (int)(12 * scale);
  ShotPaint.Icon(g, "chevron", new RectangleF(Width - size - 10 * scale, (Height - size) / 2f, size, size), Dim(palette.Muted), 1.6f * scale);
  Ring(g, r, radius);
  g.SmoothingMode = SmoothingMode.None;
  Color back = hover && Enabled ? Mix(palette.Control, palette.Fg, down ? .12f : .06f) : palette.Control;
  using (Font small = ShotPaint.FontPx(13 * scale, FontStyle.Regular)) {
   int pad = (int)(11 * scale);
   int labelW = Compact ? 0 : TextRenderer.MeasureText(Label, small, Size.Empty, TextFormatFlags.NoPrefix).Width;
   TextFormatFlags f = TextFormatFlags.VerticalCenter | TextFormatFlags.NoPrefix | TextFormatFlags.NoPadding | TextFormatFlags.SingleLine | TextFormatFlags.EndEllipsis;
   Color muted = Enabled ? palette.Muted : Mix(back, palette.Muted, .5f); Color fg = Enabled ? palette.Fg : Mix(back, palette.Fg, .5f);
   if (!Compact) { TextRenderer.DrawText(g, Label, small, new Rectangle(pad, 0, labelW + 2, Height), muted, back, f); }
   int valueX = Compact ? pad : pad + labelW + (int)(8 * scale);
   TextRenderer.DrawText(g, Text, Font, new Rectangle(valueX, 0, Width - valueX - (int)(26 * scale), Height), fg, back, f);
  }
 }
}

public class ShotMenuRenderer : ToolStripRenderer {
 readonly ShotPalette p;
 public ShotMenuRenderer(ShotPalette palette) { p = palette; }
 protected override void OnRenderToolStripBackground(ToolStripRenderEventArgs e) { e.Graphics.Clear(p.Card); }
 protected override void OnRenderToolStripBorder(ToolStripRenderEventArgs e) {
  using (Pen pen = new Pen(p.Stroke)) { e.Graphics.DrawRectangle(pen, 0, 0, e.AffectedBounds.Width - 1, e.AffectedBounds.Height - 1); }
 }
}

/// <summary>A switch that keeps CheckBox's Checked and CheckedChanged.</summary>
public class ShotToggle : CheckBox, IShotScaled {
 ShotPalette palette; float scale = 1; bool hover;
 public bool IconOnly;
 public ShotToggle() {
  SetStyle(ControlStyles.UserPaint | ControlStyles.AllPaintingInWmPaint | ControlStyles.OptimizedDoubleBuffer | ControlStyles.ResizeRedraw, true);
  Cursor = Cursors.Default; AutoSize = false;
 }
 public void Theme(ShotPalette p) { palette = p; BackColor = p.Card; ForeColor = p.Fg; }
 public void SetScale(float s) { scale = s; Font = ShotPaint.FontPx(13 * s, FontStyle.Regular); }
 public int RowWidth() {
  if (IconOnly) { return (int)(46 * scale); }
  return (int)(46 * scale) + TextRenderer.MeasureText(Text, Font, Size.Empty, TextFormatFlags.NoPrefix).Width + (int)(8 * scale);
 }
 public int RowHeight() { return (int)(32 * scale); }
 protected override void OnMouseEnter(EventArgs e) { hover = true; Invalidate(); base.OnMouseEnter(e); }
 protected override void OnMouseLeave(EventArgs e) { hover = false; Invalidate(); base.OnMouseLeave(e); }
 protected override void OnEnabledChanged(EventArgs e) { Invalidate(); base.OnEnabledChanged(e); }
 protected override void OnCheckedChanged(EventArgs e) { Invalidate(); base.OnCheckedChanged(e); }
 protected override void OnPaintBackground(PaintEventArgs e) { }
 static Color Mix(Color a, Color b, float t) { return Color.FromArgb((int)(a.R + (b.R - a.R) * t), (int)(a.G + (b.G - a.G) * t), (int)(a.B + (b.B - a.B) * t)); }
 protected override void OnPaint(PaintEventArgs e) {
  if (palette == null) { return; }
  Graphics g = e.Graphics;
  g.Clear(palette.Card);
  g.SmoothingMode = SmoothingMode.AntiAlias;
  if (hover && Enabled) { ShotPaint.Fill(g, new RectangleF(0.5f, 0.5f, Width - 1, Height - 1), 8 * scale, palette.Control, palette.Control, 0); }
  Color back = hover && Enabled ? palette.Control : palette.Card;
  float w = 30 * scale; float h = 18 * scale; float x = 8 * scale; float y = (Height - h) / 2f;
  Color track = Checked ? palette.Accent : Mix(palette.Control, palette.Fg, .18f);
  if (!Enabled) { track = Mix(back, track, .5f); }
  ShotPaint.Fill(g, new RectangleF(x, y, w, h), h / 2, track, track, 0);
  float knob = h - 4 * scale;
  float kx = Checked ? x + w - knob - 2 * scale : x + 2 * scale;
  using (SolidBrush b = new SolidBrush(Enabled ? Color.White : Mix(back, Color.White, .7f))) { g.FillEllipse(b, kx, y + 2 * scale, knob, knob); }
  if (Focused && ShowFocusCues) { using (Pen pen = new Pen(palette.Accent, 2 * scale)) { g.DrawRectangle(pen, scale, scale, Width - 2 * scale, Height - 2 * scale); } }
  g.SmoothingMode = SmoothingMode.None;
  if (IconOnly) { return; }
  TextRenderer.DrawText(g, Text, Font, new Rectangle((int)(x + w + 7 * scale), 0, Width - (int)(x + w + 7 * scale), Height), Enabled ? palette.Fg : Mix(back, palette.Fg, .5f), back, TextFormatFlags.VerticalCenter | TextFormatFlags.NoPrefix | TextFormatFlags.NoPadding | TextFormatFlags.SingleLine);
 }
}

/// <summary>The non-interactive arrow between the source and target pills.</summary>
public class ShotArrow : Control, IShotScaled {
 ShotPalette palette; float scale = 1;
 public ShotArrow() { SetStyle(ControlStyles.Selectable, false); SetStyle(ControlStyles.UserPaint | ControlStyles.AllPaintingInWmPaint | ControlStyles.OptimizedDoubleBuffer, true); TabStop = false; Cursor = Cursors.Default; }
 public void Theme(ShotPalette p) { palette = p; }
 public void SetScale(float s) { scale = s; }
 public int RowWidth() { return (int)(28 * scale); }
 public int RowHeight() { return (int)(36 * scale); }
 protected override void OnPaint(PaintEventArgs e) {
  if (palette == null) { return; }
  e.Graphics.Clear(palette.Card);
  e.Graphics.SmoothingMode = SmoothingMode.AntiAlias;
  float size = 16 * scale;
  ShotPaint.Icon(e.Graphics, "arrow", new RectangleF((Width - size) / 2, (Height - size) / 2, size, size), palette.Muted, 1.6f * scale);
 }
}

/// <summary>The two-row toolbar. It paints the scene under its own corners and shadow, so the card reads as floating.</summary>
public class ShotBar : Panel {
 readonly ShotForm surface;
 readonly ShotPalette palette;
 readonly List<Control> top = new List<Control>();
 readonly List<Control> bottomLeft = new List<Control>();
 readonly List<Control> bottomRight = new List<Control>();
 readonly Label status = new Label();
 readonly Label detail = new Label();
 readonly ToolTip tips = new ToolTip();
 Control action;
 float scale = 1; int widthLimit = int.MaxValue; string statusText = ""; bool statusError;
 Rectangle card; int divider;
 public ShotBar(ShotForm form, ShotPalette p) {
  surface = form; palette = p;
  SetStyle(ControlStyles.UserPaint | ControlStyles.AllPaintingInWmPaint | ControlStyles.OptimizedDoubleBuffer | ControlStyles.ResizeRedraw, true);
  Cursor = Cursors.Default; Visible = false; TabStop = false;
  foreach (Label l in new Label[] { status, detail }) {
   l.BackColor = p.Card; l.ForeColor = p.Muted; l.UseMnemonic = false; l.AutoSize = false; Controls.Add(l);
  }
  status.TextAlign = ContentAlignment.MiddleRight; status.AutoEllipsis = true;
  detail.TextAlign = ContentAlignment.TopLeft; detail.Visible = false; detail.AutoEllipsis = true;
 }
 void Own(Control c) { ((IShotScaled)c).Theme(palette); Controls.Add(c); }
 public void AddTop(Control c) { top.Add(c); Own(c); }
 public void SetAction(Control c) { action = c; Own(c); }
 public void AddBottomLeft(Control c) { bottomLeft.Add(c); Own(c); }
 public void AddBottomRight(Control c) { bottomRight.Add(c); Own(c); }
 static int W(Control c) { ShotButton b = c as ShotButton; if (b != null) { return b.RowWidth(); } ShotToggle t = c as ShotToggle; if (t != null) { return t.RowWidth(); } return ((ShotArrow)c).RowWidth(); }
 static int H(Control c) { ShotButton b = c as ShotButton; if (b != null) { return b.RowHeight(); } ShotToggle t = c as ShotToggle; if (t != null) { return t.RowHeight(); } return ((ShotArrow)c).RowHeight(); }
 void SetCompact(Control c, bool compact) {
  ShotButton b = c as ShotButton;
  if (b != null) { b.IconOnly = compact && b.CanCompact; }
  ShotToggle t = c as ShotToggle;
  if (t != null) { t.IconOnly = compact; }
  tips.SetToolTip(c, compact ? c.Text : "");
 }
 int TopWidth(int gapSmall) { int total = 0; foreach (Control c in top) { total += W(c) + (c is ShotArrow ? 0 : gapSmall); } return total; }
 static int PickerWidth(List<ShotPicker> pickers) { int total = 0; foreach (ShotPicker p in pickers) { total += p.RowWidth(); } return total; }
 void ScaleAll() {
  foreach (Control c in Controls) { IShotScaled s = c as IShotScaled; if (s != null) { s.SetScale(scale); } }
  status.Font = ShotPaint.FontPx(13 * scale, FontStyle.Regular);
  detail.Font = status.Font;
 }
 public void SetStatus(string text, bool error) { statusText = text ?? ""; statusError = error; Arrange(scale, widthLimit); }
 public void Arrange(float s, int maxWidth) {
  scale = s; widthLimit = maxWidth;
  SuspendLayout();
  ScaleAll();
  int shadow = (int)(16 * s); int inner = (int)(10 * s); int gap = (int)(8 * s); int gapSmall = (int)(2 * s);
  int width = Math.Min((int)(700 * s), Math.Max((int)(320 * s), maxWidth - shadow * 2));
  int contentW = width - inner * 2;
  // Row 2 falls back to icons with tooltips when a narrow monitor cannot hold its words.
  List<Control> second = new List<Control>(bottomLeft); second.AddRange(bottomRight);
  foreach (Control c in second) { SetCompact(c, false); }
  int need = 0; foreach (Control c in second) { need += W(c) + gapSmall; }
  bool compact = need > contentW - gap;
  foreach (Control c in second) { SetCompact(c, compact); }
  int x = inner + shadow; int y = inner + shadow; int rowOne = (int)(36 * s);
  // Row 1 gives way before anything overlaps the action: captions go first, then the pills narrow.
  int actionW = action == null ? 0 : W(action);
  int room = contentW - actionW - gap;
  int pickerCap = room;
  List<ShotPicker> pickers = new List<ShotPicker>();
  foreach (Control c in top) { ShotPicker p = c as ShotPicker; if (p != null) { p.Compact = false; pickers.Add(p); } }
  if (TopWidth(gapSmall) > room) {
   foreach (ShotPicker p in pickers) { p.Compact = true; }
   if (TopWidth(gapSmall) > room && pickers.Count > 0) { pickerCap = Math.Max(0, (room - (TopWidth(gapSmall) - PickerWidth(pickers))) / pickers.Count); }
  }
  foreach (Control c in top) {
   int w = c is ShotPicker ? Math.Min(W(c), pickerCap) : W(c);
   c.SetBounds(x, y + (rowOne - H(c)) / 2, w, H(c)); x += w + (c is ShotArrow ? 0 : gapSmall);
  }
  if (action != null) { action.SetBounds(inner + shadow + contentW - actionW, y + (rowOne - H(action)) / 2, actionW, H(action)); }
  int slot = inner + shadow + contentW - actionW - gap - x - gap;
  int textW = statusText.Length == 0 ? 0 : TextRenderer.MeasureText(statusText, status.Font, Size.Empty, TextFormatFlags.NoPrefix).Width;
  bool wrap = textW > slot;
  status.Visible = !wrap && textW > 0;
  status.Text = wrap ? "" : statusText;
  status.ForeColor = statusError ? palette.Err : palette.Muted;
  status.SetBounds(x + gap, y, Math.Max(0, slot), rowOne);
  int rowTwoY = y + rowOne + (int)(17 * s);
  divider = y + rowOne + (int)(8 * s);
  int rowTwo = (int)(32 * s);
  int lx = inner + shadow;
  foreach (Control c in bottomLeft) { int w = W(c); c.SetBounds(lx, rowTwoY, w, rowTwo); lx += w + gapSmall; }
  int rx = inner + shadow + contentW;
  for (int i = bottomRight.Count - 1; i >= 0; i--) { Control c = bottomRight[i]; int w = W(c); rx -= w; c.SetBounds(rx, rowTwoY, w, rowTwo); rx -= gapSmall; }
  int bottom = rowTwoY + rowTwo + inner;
  detail.Visible = wrap;
  if (wrap) {
   detail.Text = statusText; detail.ForeColor = statusError ? palette.Err : palette.Muted;
   int lineW = contentW - (int)(16 * s);
   // At most five lines, so a long provider error can't push the toolbar off the monitor; the tooltip has the rest.
   int lineH = TextRenderer.MeasureText("译", detail.Font).Height;
   int detailH = Math.Min(TextRenderer.MeasureText(statusText, detail.Font, new Size(lineW, 0), TextFormatFlags.WordBreak | TextFormatFlags.NoPrefix).Height, lineH * 5) + (int)(2 * s);
   tips.SetToolTip(detail, statusText);
   detail.SetBounds(inner + shadow + (int)(5 * s), rowTwoY + rowTwo + (int)(4 * s), lineW, detailH);
   bottom = detail.Bottom + inner;
  }
  card = new Rectangle(shadow, shadow, width, bottom - shadow);
  Size = new Size(width + shadow * 2, bottom + shadow);
  ResumeLayout(false);
  Invalidate();
 }
 /// <summary>Under the selection, else above it, else inside its bottom edge. Both rectangles are in the surface's coordinates.</summary>
 public void Place(Rectangle selection, Rectangle area) {
  int shadow = (int)(16 * scale); int gap = (int)(8 * scale);
  int x = Math.Max(area.Left - shadow, Math.Min(selection.X - shadow, area.Right - Width + shadow));
  int y = selection.Bottom + gap - shadow;
  if (y + Height - shadow > area.Bottom) {
   y = selection.Top - gap - Height + shadow;
   if (y + shadow < area.Top) { y = area.Bottom - Height + shadow - gap; }
  }
  Location = new Point(x, y);
  Parent.Invalidate();
  Invalidate();
 }
 protected override void OnPaintBackground(PaintEventArgs e) { }
 protected override void OnPaint(PaintEventArgs e) {
  Graphics g = e.Graphics;
  GraphicsState state = g.Save();
  g.TranslateTransform(-Left, -Top);
  Rectangle clip = e.ClipRectangle; clip.Offset(Left, Top);
  surface.PaintScene(g, clip);
  g.Restore(state);
  g.SmoothingMode = SmoothingMode.AntiAlias;
  float radius = 14 * scale;
  ShotPaint.Shadow(g, card, radius, scale);
  ShotPaint.Fill(g, card, radius, palette.Card, palette.Stroke, Math.Max(1, scale));
  g.SmoothingMode = SmoothingMode.None;
  int inner = (int)(10 * scale);
  using (Pen pen = new Pen(palette.Stroke, Math.Max(1, scale))) { g.DrawLine(pen, card.X + inner, divider, card.Right - inner, divider); }
 }
}

/// <summary>The pinned image's right-click menu: the toolbar's menu colours, with the hovered row filled.</summary>
public class ShotPinMenuRenderer : ShotMenuRenderer {
 readonly ShotPalette p;
 public ShotPinMenuRenderer(ShotPalette palette) : base(palette) { p = palette; }
 protected override void OnRenderMenuItemBackground(ToolStripItemRenderEventArgs e) {
  if (!e.Item.Selected || !e.Item.Enabled) { return; }
  e.Graphics.SmoothingMode = SmoothingMode.AntiAlias;
  ShotPaint.Fill(e.Graphics, new RectangleF(1, 0, e.Item.Width - 2, e.Item.Height), 6, p.Control, p.Control, 0);
 }
 protected override void OnRenderItemText(ToolStripItemTextRenderEventArgs e) { e.TextColor = p.Fg; base.OnRenderItemText(e); }
}

/// <summary>A screenshot pinned to the screen. No title bar and no frame: the image fills the window and
/// only the drop shadow sets it off from what is behind. Drag it to move it, drag an edge to resize it
/// (the aspect is kept), double-click or Esc closes it; a close button shows while the pointer is over
/// it, and right-click copies or saves the image.</summary>
public class ShotPin : Form {
 const int WM_NCHITTEST = 0x84, WM_SIZING = 0x214, WM_NCLBUTTONDOWN = 0xA1, HTCAPTION = 2;
 [DllImport("user32.dll")] static extern bool ReleaseCapture();
 [DllImport("user32.dll")] static extern IntPtr SendMessage(IntPtr hwnd, int msg, IntPtr wParam, IntPtr lParam);
 [StructLayout(LayoutKind.Sequential)] struct RECT { public int Left; public int Top; public int Right; public int Bottom; }
 readonly Bitmap image; readonly ShotPalette palette; readonly float scale; readonly float aspect;
 bool hover, overClose;
 DateTime lastDown = DateTime.MinValue; Point lastDownAt;
 public ShotPin(Bitmap image, ShotPalette palette, float scale) {
  this.image = image; this.palette = palette; this.scale = scale;
  aspect = image.Width / (float)Math.Max(1, image.Height);
  SetStyle(ControlStyles.UserPaint | ControlStyles.AllPaintingInWmPaint | ControlStyles.OptimizedDoubleBuffer | ControlStyles.ResizeRedraw, true);
  AutoScaleMode = AutoScaleMode.None;
  FormBorderStyle = FormBorderStyle.None;
  StartPosition = FormStartPosition.Manual;
  TopMost = true; KeyPreview = true; ShowIcon = false;
  Text = "Tolk 截图";
  BackColor = palette.Card;
  ClientSize = image.Size;
  ContextMenuStrip = BuildMenu();
 }
 // CS_DROPSHADOW: the system shadow, the window's only edge.
 protected override CreateParams CreateParams { get { CreateParams cp = base.CreateParams; cp.ClassStyle |= 0x20000; return cp; } }
 Rectangle CloseBox {
  get { int size = (int)(26 * scale), inset = (int)(8 * scale); return new Rectangle(ClientSize.Width - size - inset, inset, size, size); }
 }
 ContextMenuStrip BuildMenu() {
  ContextMenuStrip menu = new ContextMenuStrip();
  menu.Renderer = new ShotPinMenuRenderer(palette);
  menu.BackColor = palette.Card;
  menu.ShowImageMargin = false;
  menu.Padding = new Padding((int)(4 * scale));
  menu.Font = ShotPaint.FontPx(14 * scale, FontStyle.Regular);
  AddItem(menu, "复制", delegate { CopyImage(); });
  AddItem(menu, "另存为…", delegate { SaveImage(); });
  AddItem(menu, "关闭", delegate { Close(); });
  return menu;
 }
 void AddItem(ContextMenuStrip menu, string text, EventHandler click) {
  ToolStripMenuItem item = new ToolStripMenuItem(text);
  item.ForeColor = palette.Fg;
  item.Padding = new Padding((int)(6 * scale), (int)(4 * scale), (int)(16 * scale), (int)(4 * scale));
  item.Click += click;
  menu.Items.Add(item);
 }
 void CopyImage() {
  try { Clipboard.SetImage(image); } catch (Exception ex) { MessageBox.Show(this, "复制失败：" + ex.Message, "Tolk"); }
 }
 void SaveImage() {
  using (SaveFileDialog dialog = new SaveFileDialog()) {
   dialog.Filter = "PNG 图片|*.png";
   dialog.FileName = "Tolk_" + DateTime.Now.ToString("yyyyMMdd_HHmmss") + ".png";
   if (dialog.ShowDialog(this) != DialogResult.OK) { return; }
   try { image.Save(dialog.FileName, ImageFormat.Png); } catch (Exception ex) { MessageBox.Show(this, "保存失败：" + ex.Message, "Tolk"); }
  }
 }
 protected override bool ProcessCmdKey(ref Message msg, Keys keyData) {
  if (keyData == Keys.Escape) { Close(); return true; }
  if (keyData == (Keys.Control | Keys.C)) { CopyImage(); return true; }
  return base.ProcessCmdKey(ref msg, keyData);
 }
 protected override void OnPaint(PaintEventArgs e) {
  Graphics g = e.Graphics;
  bool actual = ClientSize == image.Size;
  g.InterpolationMode = actual ? InterpolationMode.NearestNeighbor : InterpolationMode.HighQualityBicubic;
  g.PixelOffsetMode = PixelOffsetMode.Half;
  g.DrawImage(image, new Rectangle(Point.Empty, ClientSize));
  if (!hover) { return; }
  Rectangle box = CloseBox;
  g.SmoothingMode = SmoothingMode.AntiAlias;
  g.PixelOffsetMode = PixelOffsetMode.Default;
  using (SolidBrush b = new SolidBrush(Color.FromArgb(overClose ? 210 : 150, 0, 0, 0))) { g.FillEllipse(b, box); }
  ShotPaint.Icon(g, "close", box, Color.White, Math.Max(1.5f, 1.8f * scale));
 }
 void RepaintClose() { Rectangle r = CloseBox; r.Inflate(2, 2); Invalidate(r); }
 protected override void OnMouseMove(MouseEventArgs e) {
  base.OnMouseMove(e);
  bool over = CloseBox.Contains(e.Location);
  if (!hover || over != overClose) { hover = true; overClose = over; RepaintClose(); }
  Cursor = over ? Cursors.Hand : Cursors.Default;
 }
 protected override void OnMouseLeave(EventArgs e) {
  base.OnMouseLeave(e);
  if (hover) { hover = false; overClose = false; RepaintClose(); }
 }
 protected override void OnMouseDown(MouseEventArgs e) {
  base.OnMouseDown(e);
  if (e.Button != MouseButtons.Left || CloseBox.Contains(e.Location)) { return; }
  // Counted here: the move loop below swallows the button-up, so WinForms never sees a double-click.
  Size slop = SystemInformation.DoubleClickSize;
  bool twice = (DateTime.Now - lastDown).TotalMilliseconds <= SystemInformation.DoubleClickTime
   && Math.Abs(e.X - lastDownAt.X) <= slop.Width && Math.Abs(e.Y - lastDownAt.Y) <= slop.Height;
  if (twice) { Close(); return; }
  lastDown = DateTime.Now; lastDownAt = e.Location;
  Point before = Location;
  ReleaseCapture();
  SendMessage(Handle, WM_NCLBUTTONDOWN, (IntPtr)HTCAPTION, IntPtr.Zero); // returns once the button is up
  if (Location != before) { lastDown = DateTime.MinValue; } // a drag, not the first half of a double-click
 }
 protected override void OnMouseUp(MouseEventArgs e) {
  base.OnMouseUp(e);
  if (e.Button == MouseButtons.Left && CloseBox.Contains(e.Location)) { Close(); }
 }
 protected override void WndProc(ref Message m) {
  if (m.Msg == WM_NCHITTEST) {
   base.WndProc(ref m);
   int lp = unchecked((int)m.LParam.ToInt64());
   Point p = PointToClient(new Point((short)(lp & 0xFFFF), (short)((lp >> 16) & 0xFFFF)));
   int grip = Math.Max(4, (int)(6 * scale));
   bool l = p.X < grip, r = p.X >= ClientSize.Width - grip, t = p.Y < grip, b = p.Y >= ClientSize.Height - grip;
   int hit = t && l ? 13 : t && r ? 14 : b && l ? 16 : b && r ? 17 : l ? 10 : r ? 11 : t ? 12 : b ? 15 : 0;
   if (hit != 0) { m.Result = (IntPtr)hit; }
   return;
  }
  if (m.Msg == WM_SIZING) {
   // Keep the image's aspect while an edge is dragged; the opposite edge stays put.
   RECT rc = (RECT)Marshal.PtrToStructure(m.LParam, typeof(RECT));
   int edge = m.WParam.ToInt32();
   float min = 40 * scale;
   float w = rc.Right - rc.Left, h = rc.Bottom - rc.Top;
   if (edge == 3 || edge == 6) { w = h * aspect; } else { h = w / aspect; }
   if (h < min) { h = min; w = h * aspect; }
   if (w < min) { w = min; h = w / aspect; }
   int iw = (int)Math.Round(w), ih = (int)Math.Round(h);
   if (edge == 1 || edge == 4 || edge == 7) { rc.Left = rc.Right - iw; } else { rc.Right = rc.Left + iw; }
   if (edge == 3 || edge == 4 || edge == 5) { rc.Top = rc.Bottom - ih; } else { rc.Bottom = rc.Top + ih; }
   Marshal.StructureToPtr(rc, m.LParam, false);
   m.Result = (IntPtr)1;
   return;
  }
  base.WndProc(ref m);
 }
 protected override void OnFormClosed(FormClosedEventArgs e) {
  base.OnFormClosed(e);
  if (ContextMenuStrip != null) { ContextMenuStrip.Dispose(); }
  image.Dispose();
 }
}
'@
