# Чтение кадров GL/GLES

Пакет проекта Granio; исходный репозиторий обёрток —
[go-gl/gl](https://github.com/go-gl/gl). Цель дополнения — проверяемое чтение
плотного RGBA8 в Go-буфер с восстановлением pixel-pack состояния.

`RGBA8Size(width, height)` отвергает нулевые, отрицательные размеры,
переполнение `GLsizei` и объёма Go-буфера. `New(context, procedures)` принимает
снимок возможностей и таблицу функций того же текущего контекста.

`Reader.ReadRGBA8(x, y, width, height, dst)` проверяет диапазон координат и
длину `dst`, сохраняет `PACK_ALIGNMENT`, поддерживаемые row length/skips и
привязку pixel-pack buffer. Перед `ReadPixels` он устанавливает плотную раскладку
и отвязывает PBO, чтобы Go-адрес не интерпретировался как смещение в GPU-буфере.
После чтения состояние восстанавливается, в том числе при ошибке или panic.
Успешное повторное чтение с готовым `dst` не выделяет Go-память.

Строки результата идут снизу вверх, как у `glReadPixels`. Пакет не выбирает
FBO/read buffer, не переворачивает изображение и не преобразует alpha.
Вызывающая сторона предоставляет полный читаемый framebuffer, правильный
поток контекста и чистое состояние GL-ошибок. Имеющиеся и новые GL-ошибки
возвращаются как `GLError`; `GetError` потребляет код. `Reader` принадлежит
одному контексту и потоку; конкурентное использование не допускается.

ES 2 и WebGL 1 не запрашивают параметры, которых нет в их API. Для ES 2
проверяются расширения
[NV_pack_subimage](https://registry.khronos.org/OpenGL/extensions/NV/NV_pack_subimage.txt)
и [NV_pixel_buffer_object](https://registry.khronos.org/OpenGL/extensions/NV/NV_pixel_buffer_object.txt).
Поддержка описания WebGL проверена тестами адаптера; браузерный runtime в
этом этапе не проверялся. Нативный OpenGL проверяется через драйвер Granio
на Windows, X11 и Wayland; GLES 2 — на X11 и Wayland. Точная матрица приведена в
[отчёте Granio](https://github.com/Uzer007-admin/granio/blob/dev/granio/docs/GL_CAPTURE_REPAIR.md).
